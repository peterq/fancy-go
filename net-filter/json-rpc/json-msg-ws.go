package json_rpc

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/peterq/fancy-go/cond-chan"
	"github.com/peterq/fancy-go/net-filter/websocket-util"
	"github.com/pkg/errors"
	"golang.org/x/net/websocket"
)

var _ JsonMessageChannel = (*wsChannel)(nil)

type wsChannel struct {
	ctx      context.Context
	writeBin func(bin []byte) error
	readMsg  func(ctx context.Context, msg *Message) error
	close    func() error
}

func (w *wsChannel) ReadCtx(ctx context.Context, msg *Message) error {
	return w.readMsg(ctx, msg)
}

func (w *wsChannel) WriteCtx(ctx context.Context, msg *Message) error {
	bin, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return w.writeBin(bin)
}

func (w *wsChannel) Close() error {
	return w.close()
}

func (w *wsChannel) Closed() bool {
	return w.ctx.Err() != nil
}

func WebSocketConn2MessageChannel(conn *websocket.Conn) (JsonMessageChannel, JsonMessageChannel) {
	writeMutex := sync.Mutex{}
	ctx, cancel := context.WithCancel(context.Background())
	closeFn := func() error {
		cancel()
		return conn.Close()
	}
	writeBin := func(bin []byte) error {
		writeMutex.Lock()
		defer writeMutex.Unlock()
		conn.PayloadType = websocket.TextFrame
		_, err := conn.Write(bin)
		if err != nil {
			closeFn()
			return err
		}
		return err
	}

	type messageRole int
	const (
		roleServer messageRole = 1
		roleClient messageRole = 2
	)
	var lastMessage Message
	var lastMessageRole messageRole
	var lastMsgTakenCond = cond_chan.NewCond()
	var readMutex sync.Mutex
	var readToLastMsgLocked = func(readCtx context.Context) error {
		if lastMessageRole != 0 {
			panic("lastMessageRole has not been taken")
		}
		_, bin, err := websocket_util.ReceiveFullFrame(conn, readCtx)
		if err != nil {
			closeFn()
			return errors.Wrap(err, "read json message")
		}
		lastMessage = Message{}
		err = json.Unmarshal(bin, &lastMessage)
		if err != nil {
			return errors.Wrap(err, "unmarshal json message")
		}
		// check msg role
		if lastMessage.ID > 0 { // has id, is request/or response
			if lastMessage.Method != "" { // has method, is request
				lastMessageRole = roleServer // server can receive request
			} else { // no method, is response
				lastMessageRole = roleClient // client can receive response
			}
		} else { // no id, is notification
			if lastMessage.Method != "" { // has method, is notification
				lastMessageRole = roleClient // client can receive notification
			} else { // no method, invalid message
				return errors.New("invalid message")
			}
		}
		return nil
	}
	var readFn = func(role messageRole, readCtx context.Context) (Message, error) {
		readMutex.Lock()
		defer readMutex.Unlock()

		for {
			if lastMessageRole == role {
				msg := lastMessage
				lastMessageRole = 0
				lastMsgTakenCond.Broadcast()
				return msg, nil
			}
			if lastMessageRole != 0 { // wait for last message taken
				ch := lastMsgTakenCond.Wait()
				// unlock to wait
				readMutex.Unlock()
				var waitErr error
				select {
				case <-time.After(time.Second * 10):
					waitErr = errors.New("last message not taken")
				case <-ch: // last message taken
				case <-readCtx.Done():
					waitErr = readCtx.Err()
				case <-ctx.Done():
					waitErr = ctx.Err()
				}
				// lock again
				readMutex.Lock()
				if waitErr != nil {
					return Message{}, waitErr
				}
				continue
			}
			// read to last message
			err := readToLastMsgLocked(readCtx)
			if err != nil {
				return Message{}, err
			}
		}
	}

	return &wsChannel{
			ctx:      ctx,
			writeBin: writeBin,
			readMsg: func(ctx context.Context, msg *Message) error {
				m, err := readFn(roleServer, ctx)
				if err != nil {
					return err
				}
				*msg = m
				return nil
			},
			close: closeFn,
		}, &wsChannel{
			ctx:      ctx,
			writeBin: writeBin,
			readMsg: func(ctx context.Context, msg *Message) error {
				m, err := readFn(roleClient, ctx)
				if err != nil {
					return err
				}
				*msg = m
				return nil
			},
			close: closeFn,
		}
}

type WsHttpHandler struct {
	OriginWhitelist []string
	Protocols       []string
	Handshake       func(config *websocket.Config, request *http.Request) (err error)
	OnChannel       func(ctx context.Context, serverChannel JsonMessageChannel, clientChannel JsonMessageChannel) ServerSession

	wsServer websocket.Server
	initOnce sync.Once
}

func (h *WsHttpHandler) Init() {
	h.initOnce.Do(func() {
		h.wsServer = websocket.Server{
			Handshake: func(config *websocket.Config, request *http.Request) (err error) {
				defer func() {
					if err != nil {
						log.Println("websocket handshake error:", err)
					}
				}()

				// check origin
				if len(h.OriginWhitelist) > 0 {
					org := request.Header.Get("Origin")
					if org == "" {
						return errors.New("origin is empty")
					}
					origin, err := url.Parse(org)
					if err != nil {
						return errors.Wrap(err, "parse origin error")
					}
					var originHost string
					hostname, _, err := net.SplitHostPort(origin.Host)
					if err != nil {
						hostname = origin.Host
					}
					for _, v := range h.OriginWhitelist {
						if strings.HasSuffix(hostname, v) {
							originHost = v
							break
						}
					}
					if originHost == "" {
						return errors.New("origin not in white list: " + hostname)
					}
				}

				if len(h.Protocols) > 0 {
					protocol := ""
					for _, v := range config.Protocol {
						for _, p := range h.Protocols {
							if v == p {
								protocol = v
								break
							}
						}
						if protocol != "" {
							break
						}
					}
					if protocol == "" {
						return errors.New("protocol not found")
					}
					config.Protocol = []string{protocol}
				}
				if f := h.Handshake; f != nil {
					return f(config, request)
				}
				return nil
			},
			Handler: h.handleWs,
		}
	})
}

func (h *WsHttpHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.Init()
	h.wsServer.ServeHTTP(writer, request)
}

type ServerSession interface {
	Serve(ctx context.Context) (err error)
}
type ctxKey string

const ctxKeyWsConn = ctxKey("wsConn")

func WsConnFromCtx(ctx context.Context) *websocket.Conn {
	return ctx.Value(ctxKeyWsConn).(*websocket.Conn)
}

func (h *WsHttpHandler) handleWs(ws *websocket.Conn) {
	defer ws.Close()
	ctx, cancel := context.WithCancel(ws.Request().Context())
	defer cancel()
	serverChannel, clientChannel := WebSocketConn2MessageChannel(ws)
	ctx = context.WithValue(ctx, ctxKeyWsConn, ws)
	ss := h.OnChannel(ctx, serverChannel, clientChannel)
	if ss == nil {
		log.Println("OnChannel return nil")
		return
	}
	err := ss.Serve(ws.Request().Context())
	log.Println("session end:", err)
}

var _ JsonMessageChannel = (*autoReconnectChannel)(nil)
var _ ChannelWithBrokenSignal = (*autoReconnectChannel)(nil)

func NewAutoReconnectChannel(
	createFn func(ctx context.Context) (JsonMessageChannel, error),
) JsonMessageChannel {
	return &autoReconnectChannel{
		createFn:     createFn,
		createSignal: cond_chan.NewCond(),
		brokenSignal: cond_chan.NewCond(),
	}
}

type autoReconnectChannel struct {
	createFn     func(ctx context.Context) (JsonMessageChannel, error)
	mu           sync.Mutex
	createSignal cond_chan.Cond
	creating     bool
	closed       bool
	brokenSignal cond_chan.Cond
	inner        JsonMessageChannel
}

func (c *autoReconnectChannel) BrokenSignal() <-chan bool {
	return c.brokenSignal.Wait()
}

func (c *autoReconnectChannel) WriteCtxWithBrokenSignal(ctx context.Context, msg *Message) (<-chan bool, error) {
	brokenSignal, inner, err := c.getInner(ctx)
	if err != nil {
		return nil, err
	}
	if hasBrokenSignal, ok := inner.(ChannelWithBrokenSignal); ok {
		return hasBrokenSignal.WriteCtxWithBrokenSignal(ctx, msg)
	}
	return brokenSignal, inner.WriteCtx(ctx, msg)
}

func (c *autoReconnectChannel) getInner(ctx context.Context) (<-chan bool, JsonMessageChannel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inner != nil && c.inner.Closed() {
		c.inner = nil
		c.brokenSignal.Broadcast()
	}
	c.closed = false
	if c.inner != nil {
		if hasBrokenSignal, ok := c.inner.(ChannelWithBrokenSignal); ok {
			return hasBrokenSignal.BrokenSignal(), c.inner, nil
		}
		return c.brokenSignal.Wait(), c.inner, nil
	}
	for {
		if c.inner != nil && !c.inner.Closed() {
			if hasBrokenSignal, ok := c.inner.(ChannelWithBrokenSignal); ok {
				return hasBrokenSignal.BrokenSignal(), c.inner, nil
			}
			return c.brokenSignal.Wait(), c.inner, nil
		}

		if c.creating {
			creatingSignal := c.createSignal.Wait()
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				c.mu.Lock()
				return nil, nil, ctx.Err()
			case <-creatingSignal:
				c.mu.Lock()
				continue
			}
		} else {
			c.creating = true
			c.mu.Unlock()
			var inner JsonMessageChannel
			var err error
			func() {
				defer func() {
					c.mu.Lock()
					c.creating = false
					c.createSignal.Broadcast()
				}()
				inner, err = c.createFn(ctx)
			}()
			if err != nil {
				return nil, nil, err
			}
			c.inner = inner
			if hasBrokenSignal, ok := c.inner.(ChannelWithBrokenSignal); ok {
				return hasBrokenSignal.BrokenSignal(), c.inner, nil
			}
			return c.brokenSignal.Wait(), c.inner, nil
		}
	}
}

func (c *autoReconnectChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.inner != nil {
		return c.inner.Close()
	}
	return nil
}

func (c *autoReconnectChannel) ReadCtx(ctx context.Context, msg *Message) error {
	for ctx.Err() == nil {
		_, inner, err := c.getInner(ctx)
		if err != nil {
			log.Println("get inner error:", err)
			time.Sleep(time.Second)
			continue
		}
		err = inner.ReadCtx(ctx, msg)
		if err != nil {
			log.Println("read error:", err)
			time.Sleep(time.Second)
			continue
		}
		return nil
	}
	return ctx.Err()
}

func (c *autoReconnectChannel) WriteCtx(ctx context.Context, msg *Message) error {
	_, inner, err := c.getInner(ctx)
	if err != nil {
		return err
	}
	return inner.WriteCtx(ctx, msg)
}

func (c *autoReconnectChannel) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func NewAutoReconnectChannelFromWebsocketAddr(
	addr string,
	handleServerChannel func(ctx context.Context, serverChannel JsonMessageChannel),
) JsonMessageChannel {
	return NewAutoReconnectChannel(func(ctx context.Context) (JsonMessageChannel, error) {
		conn, err := websocket.Dial(addr, "", addr)
		if err != nil {
			return nil, errors.Wrap(err, "websocket dial error")
		}
		server, cli := WebSocketConn2MessageChannel(conn)
		if handleServerChannel != nil {
			go handleServerChannel(ctx, server)
		}
		return cli, nil
	})
}
