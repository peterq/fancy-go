package ws_agent

import (
	"context"
	"github.com/peterq/fancy-go/net-filter/websocket-util"
	"github.com/pkg/errors"
	"golang.org/x/net/websocket"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

type PipeDataEvent struct {
	Type byte
	Data []byte
}

type PipeHooks struct {
	OnReceive func(event *PipeDataEvent)
	OnSend    func(event *PipeDataEvent)
}

type WsAgentOptions struct {
	GetTarget func(request *http.Request) (string, error)
	PipeHooks func(request *http.Request, src *websocket.Conn, dest *websocket.Conn) (PipeHooks, error)
}

func NewAgent(opt WsAgentOptions) WsAgent {
	return &wsAgent{
		options: opt,
	}
}

type WsAgent interface {
	ServeHttp(http.ResponseWriter, *http.Request)
}

var _ WsAgent = (*wsAgent)(nil)

type wsAgent struct {
	options WsAgentOptions
}

func (w *wsAgent) ServeHttp(writer http.ResponseWriter, request *http.Request) {
	handleError := func(err error) {
		log.Println("handle error", err, request.URL.Query().Get("dest"))
		writer.Header().Set("X-Server-Error", err.Error())
		writer.WriteHeader(500)
	}
	target, err := w.options.GetTarget(request)
	if err != nil {
		handleError(errors.Wrap(err, "get target error"))
		return
	}
	conf, err := websocket.NewConfig(target, request.Header.Get("origin"))
	if err != nil {
		handleError(errors.Wrap(err, "new target config error"))
		return
	}
	conf.Protocol = strings.Split(request.Header.Get("Sec-WebSocket-Protocol"), ", ")
	conf.Header = http.Header{
		"Sec-WebSocket-Extensions": request.Header["Sec-Websocket-Extensions"],
	}
	for k, v := range request.Header {
		if strings.HasPrefix(k, "Sec-Websocket-") {
			continue
		}
		if k == "Origin" || k == "Connection" || k == "Upgrade" {
			continue
		}
		conf.Header[k] = v
	}
	targetConn, err := websocket.DialConfig(conf)
	if err != nil {
		handleError(errors.Wrap(err, "dail target error"))
		return
	}
	targetConn.PayloadType = websocket.BinaryFrame
	defer targetConn.Close()
	var handled bool

	s := websocket.Server{
		Handler: func(conn *websocket.Conn) {
			handled = true
			conn.PayloadType = websocket.BinaryFrame
			err = w.startPipe(request, conn, targetConn)
			log.Println("pipe end", err)
		},
		Handshake: func(config *websocket.Config, request *http.Request) error {
			config.Protocol = targetConn.Config().Protocol
			return nil
		},
	}
	s.ServeHTTP(writer, request)
	if !handled {
		log.Println("source ws hand shake error")
	}
}

func (w *wsAgent) startPipe(request *http.Request, src *websocket.Conn, dest *websocket.Conn) error {
	var hooks PipeHooks
	if w.options.PipeHooks != nil {
		var err error
		hooks, err = w.options.PipeHooks(request, src, dest)
		if err != nil {
			return errors.Wrap(err, "get pipe hooks error")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pipe := func(a *websocket.Conn, b *websocket.Conn, cb func(event *PipeDataEvent)) error {
		frameA := websocket_util.NewFrameIo(a)
		frameB := websocket_util.NewFrameIo(b)
		for {
			payloadType, bin, err := frameA.ReadCtx(ctx)
			if err != nil {
				return errors.Wrap(err, "read error")
			}
			if cb != nil {
				evt := &PipeDataEvent{
					Type: payloadType,
					Data: bin,
				}
				cb(evt)
				if evt.Data == nil {
					continue
				}
				bin = evt.Data
			}
			err = frameB.WriteCtx(payloadType, bin, ctx)
			if err != nil {
				return errors.Wrap(err, "write error")
			}
		}
	}

	var pipeError error
	var pipeErrorSet uint32
	// pipe receive
	go func() {
		err := pipe(dest, src, hooks.OnReceive)
		if err != nil {
			if atomic.CompareAndSwapUint32(&pipeErrorSet, 0, 1) {
				pipeError = errors.Wrap(err, "pipe receive error")
				cancel()
			}
		}
	}()

	// pipe send
	err := pipe(src, dest, hooks.OnSend)
	if err != nil {
		if atomic.CompareAndSwapUint32(&pipeErrorSet, 0, 1) {
			pipeError = errors.Wrap(err, "pipe send error")
			cancel()
		}
	}
	return pipeError

}
