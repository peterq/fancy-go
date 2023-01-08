package json_rpc

import (
	"context"
	"encoding/json"
	"github.com/peterq/fancy-go/error-code"
	"github.com/pkg/errors"
	"log"
	"sync"
	"sync/atomic"
)

type Cli struct {
	messageChannel JsonMessageChannel
	ctx            context.Context
	cancel         context.CancelFunc
	seq            uint64
	callMapMux     sync.RWMutex
	callMap        map[uint64]*pendingCall
	handleEvent    func(evtType string, evtPayload json.RawMessage) error
}

type pendingCall struct {
	ch chan error
	r  interface{}
}

var NoopHandleEvent func(evtType string, evtPayload json.RawMessage) error

func NewCli(ctx context.Context, messageChannel JsonMessageChannel, handleEvent func(evtType string, evtPayload json.RawMessage) error) *Cli {
	ctx, cancel := context.WithCancel(ctx)
	c := &Cli{
		messageChannel: messageChannel,
		ctx:            ctx,
		cancel:         cancel,
		seq:            0,
		callMapMux:     sync.RWMutex{},
		callMap:        make(map[uint64]*pendingCall),
		handleEvent:    handleEvent,
	}
	go c.init()
	return c
}

func (c *Cli) init() {
	defer c.cancel()
	defer c.messageChannel.Close()
	var msg Message
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			err := c.messageChannel.ReadCtx(c.ctx, &msg)
			if err != nil {
				return
			}
			if msg.ID == 0 {
				if c.handleEvent != nil {
					err = c.handleEvent(msg.Method, msg.Params)
					if err != nil {
						log.Printf("handle event error: %v", err)
					}
				} else {
					log.Println("event ignored", msg.Method)
				}
			} else {
				c.callMapMux.RLock()
				call, ok := c.callMap[msg.ID]
				delete(c.callMap, msg.ID)
				c.callMapMux.RUnlock()
				if !ok {
					log.Println("result ignored", msg.ID)
					continue
				}
				if msg.Error != nil {
					err = error_code.NewError(msg.Error.Code, msg.Error.Message)
				} else {
					err = errors.Wrap(json.Unmarshal(msg.Result, call.r), "unmarshal result error")
				}
				call.ch <- err
			}
		}
	}
}

func (c *Cli) Emit(topic string, payload interface{}) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "marshal payload error")
	}
	msg := Message{
		Method: topic,
		Params: jsonPayload,
	}
	err = c.messageChannel.WriteCtx(c.ctx, &msg)
	if err != nil {
		return errors.Wrap(err, "write message error")
	}
	return nil
}

func (c *Cli) Call(ctx context.Context, method string, params interface{}, result interface{}) error {
	if ctx == nil || ctx == context.TODO() {
		ctx = c.ctx
	}
	jsonParams, err := json.Marshal(params)
	if err != nil {
		return errors.Wrap(err, "marshal params error")
	}
	id := atomic.AddUint64(&c.seq, 1)
	msg := Message{
		ID:     id,
		Method: method,
		Params: jsonParams,
	}
	call := &pendingCall{
		ch: make(chan error, 1),
		r:  result,
	}
	c.callMapMux.Lock()
	c.callMap[id] = call
	c.callMapMux.Unlock()
	var callRemoved bool
	defer func() {
		if !callRemoved {
			c.callMapMux.Lock()
			delete(c.callMap, id)
			c.callMapMux.Unlock()
		}
	}()
	err = c.messageChannel.WriteCtx(ctx, &msg)
	if err != nil {
		return errors.Wrap(err, "write message error")
	}
	select {
	case <-c.ctx.Done():
		return errors.Wrap(c.ctx.Err(), "context done")
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "context done")
	case err = <-call.ch:
		callRemoved = true
		return err
	}
}
