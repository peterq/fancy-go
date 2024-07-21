package json_rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/peterq/fancy-go/error-code"
	"github.com/peterq/fancy-go/utils/logger"
	"github.com/pkg/errors"
	"log"
	"reflect"
	"runtime/debug"
	"strconv"
	"sync/atomic"
)

func NewJsonRpcSession[T any](state T, funcMap map[string]RpcRawFunc[T], messageChannel JsonMessageChannel, log logger.Entry) *JsonRpcSession[T] {
	return &JsonRpcSession[T]{
		State:          state,
		Log:            log,
		funcMap:        funcMap,
		messageChannel: messageChannel,
		out:            nil,
		ctx:            nil,
		cancel:         nil,
	}
}

type MessageWithCb struct {
	Message
	Cb func(err error) error
}

type JsonRpcSession[T any] struct {
	State          T
	Log            logger.Entry
	funcMap        map[string]RpcRawFunc[T]
	messageChannel JsonMessageChannel
	out            chan MessageWithCb
	ctx            context.Context
	cancel         context.CancelFunc
}

type RpcFunc[T, U, Y any] func(s *JsonRpcSession[T], param *U) (*Y, error)
type RpcRawFunc[T any] func(s *JsonRpcSession[T], message json.RawMessage) (interface{}, error)

func RpcFuncToRaw[T, U, Y any](rpcFunc RpcFunc[T, U, Y]) RpcRawFunc[T] {
	return func(s *JsonRpcSession[T], message json.RawMessage) (interface{}, error) {
		var p U
		err := json.Unmarshal(message, &p)
		ret, err := rpcFunc(s, &p)
		return ret, err
	}
}

type Message struct {
	ID     uint64          `json:"id,omitempty"`     // Unique message identifier.
	Method string          `json:"method,omitempty"` // Event or command type.
	Params json.RawMessage `json:"params,omitempty"` // Event or command parameters.
	Result json.RawMessage `json:"result,omitempty"` // Command return values.
	Error  *Error          `json:"error,omitempty"`  // Error message.
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (w *JsonRpcSession[T]) Serve(ctx context.Context) (err error) {
	w.ctx, w.cancel = context.WithCancel(ctx)
	defer w.cancel()
	var writeError atomic.Value
	defer func() {
		l := writeError.Load()
		if e, ok := l.(error); ok {
			err = errors.Wrap(e, "write error")
		}
	}()

	w.out = make(chan MessageWithCb, 1)

	go func() (err error) {
		defer w.messageChannel.Close()
		defer w.cancel()
		defer func() {
			if err != nil {
				writeError.Store(err)
			}
		}()
		for {
			select {
			case msg := <-w.out:
				err = w.messageChannel.WriteCtx(w.ctx, &msg.Message)
				if msg.Cb != nil {
					errCb := msg.Cb(err)
					if errCb != nil {
						return errors.Wrap(errCb, "msg write callback error")
					}
				}
				if err != nil {
					return errors.Wrap(err, "write message error")
				}
			case <-w.ctx.Done():
				return
			}
		}
	}()

	var msg Message
	for {
		msg = Message{}
		err = w.messageChannel.ReadCtx(w.ctx, &msg)
		if err != nil {
			return errors.Wrap(err, "read message error")
		}
		w.handleMsg(msg)
	}
}

func (w *JsonRpcSession[T]) handleMsg(msg Message) {
	go func() {
		var result any
		var err error
		defer func() {
			if e := recover(); e != nil {
				if ee, ok := e.(error); ok {
					err = ee
				} else {
					err = errors.New(fmt.Sprint(e))
				}
				stack := string(debug.Stack())
				log.Println(stack)
				_ = error_code.From(err).WithExtra("stack_trace", stack)
			}
			var ret = Message{
				ID: msg.ID,
			}
			if err == nil {
				ret.Result, err = json.Marshal(result)
			}
			if err != nil {
				ret.Error = &Error{
					Code:    -1,
					Message: err.Error(),
				}
			}
			var resultToLog = result
			if len(ret.Result) > 1000 {
				resultToLog = "large result, length: " + strconv.Itoa(len(ret.Result))
			}
			if msg.Method == "keepalive" {
				w.Log.WithStage("call."+msg.Method).WithData("Params", msg.Params).
					WithData("Result", resultToLog).Error(err)
			} else {
				_ = w.Log.WithStage("call."+msg.Method).WithData("Params", msg.Params).
					WithData("Result", resultToLog).ErrorOrInfo(err, "ok")
			}

			w.out <- MessageWithCb{
				Message: ret,
			}
		}()
		result, err = w.handleRpcCall(msg.Method, msg.Params)
		err = ConvertRpcError(err)
	}()
}

func (w *JsonRpcSession[T]) handleRpcCall(method string, params json.RawMessage) (any, error) {
	hdlr, ok := w.funcMap[method]
	if !ok {
		log.Println(w.funcMap)
		return nil, errors.Errorf("unknown method [%s]", method)
	}
	return hdlr(w, params)
}

type internalStaticCheckHandleFunc interface {
	internalStaticCheckHandleFunc(rfv reflect.Value) error
}

var ConvertRpcError = func(err error) error {
	return err
}

var typeInternalStaticCheckHandleFunc = reflect.TypeOf((*internalStaticCheckHandleFunc)(nil)).Elem()

func (w *JsonRpcSession[T]) internalStaticCheckHandleFunc(rfv reflect.Value) error {
	rft := rfv.Type()
	fnMapType := reflect.TypeOf(w.funcMap)
	if rft.NumOut() != 1 || rft.Out(0) != fnMapType {
		return errors.Errorf("json rpc handler must have exactly a param of type %s", fnMapType.String())
	}
	return nil
}

func (w *JsonRpcSession[T]) Ctx() context.Context {
	return w.ctx
}

func (w *JsonRpcSession[T]) Emit(method string, data interface{}, cb func(err error) error) {
	go func() {
		bin, err := json.Marshal(data)
		if err != nil {
			return
		}
		msg := Message{
			ID:     0,
			Method: method,
			Params: bin,
			Result: nil,
			Error:  nil,
		}
		select {
		case <-w.ctx.Done():
		case w.out <- MessageWithCb{Message: msg, Cb: cb}:
			w.Log.WithStage("emit."+msg.Method).WithData("Params", data).Info("ok")
		}
	}()
}
