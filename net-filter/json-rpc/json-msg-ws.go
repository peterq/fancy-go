package json_rpc

import (
	"context"
	"encoding/json"
	"github.com/peterq/fancy-go/cond-chan"
	"github.com/peterq/fancy-go/net-filter/websocket-util"
	"github.com/pkg/errors"
	"golang.org/x/net/websocket"
	"sync"
	"time"
)

type wsChannel struct {
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

func WebSocketConn2MessageChannel(conn *websocket.Conn) (JsonMessageChannel, JsonMessageChannel) {
	writeMutex := sync.Mutex{}
	ctx, cancel := context.WithCancel(context.Background())
	writeBin := func(bin []byte) error {
		writeMutex.Lock()
		defer writeMutex.Unlock()
		conn.PayloadType = websocket.TextFrame
		_, err := conn.Write(bin)
		return err
	}
	closeFn := func() error {
		cancel()
		return conn.Close()
	}

	type messageRole int
	const (
		roleServer messageRole = 1
		roleClient             = 2
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
			return errors.Wrap(err, "read json message")
		}
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
