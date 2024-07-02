package grpc_util

import (
	"context"
	"fmt"
	"github.com/peterq/fancy-go/error-code"
	json_util "github.com/peterq/fancy-go/utils/json-util"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
)

type serviceInfo struct {
	// Contains the implementation for the methods in this service.
	serviceImpl interface{}
	methods     map[string]*grpc.MethodDesc
	streams     map[string]*grpc.StreamDesc
	mdata       interface{}
}

type RpcHttpServiceRegistrar interface {
	RegisterService(desc *grpc.ServiceDesc, impl interface{})
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type UnaryCallback func(ctx context.Context, service string, method string, input proto.Message, output proto.Message, err error)
type OnBeforeCallCallback func(r *http.Request, ctx context.Context, service string, method string, input proto.Message) (context.Context, error)

type RpcHttpServerConfig struct {
	Prefix        string
	OnBeforeCall  OnBeforeCallCallback
	UnaryCallback UnaryCallback
}

func NewRpcHttpServer(
	cfg RpcHttpServerConfig,
) RpcHttpServiceRegistrar {
	return &rpcHttpHandler{
		config:   cfg,
		services: make(map[string]*serviceInfo),
	}
}

type rpcHttpHandler struct {
	config   RpcHttpServerConfig
	services map[string]*serviceInfo
}

func (*rpcHttpHandler) writeError(w http.ResponseWriter, err error, code int) {
	var ret struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	ret.Code = error_code.GetCode(err)
	ret.Message = err.Error()
	w.Header().Set("X-Error-Code", fmt.Sprintf("%d", ret.Code))
	w.Header().Set("X-Error-Message", ret.Message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(json_util.JsonBin(ret))
}

func (h *rpcHttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if !strings.HasPrefix(path, h.config.Prefix) {
		h.writeError(w, errors.New("prefix not match"), http.StatusNotFound)
		return
	}
	if path == h.config.Prefix {
		w.WriteHeader(http.StatusOK)
		return
	}
	path = path[len(h.config.Prefix):]
	if path == "health" {
		w.WriteHeader(http.StatusOK)
		return
	}
	// spilt service and method
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		h.writeError(w, errors.New("invalid path"), http.StatusNotFound)
		return
	}
	methodName := parts[len(parts)-1]
	serviceName := strings.Join(parts[:len(parts)-1], "/")

	service, ok := h.services[serviceName]
	if !ok {
		h.writeError(w, errors.New(fmt.Sprintf("service %s not found", serviceName)), http.StatusNotFound)
		return
	}

	// get method
	method, ok := service.methods[methodName]
	if !ok {
		h.writeError(w, errors.New(fmt.Sprintf("method %s not found", methodName)), http.StatusNotFound)
		return
	}
	ctx := r.Context()

	isInputJson := r.Header.Get("Content-Type") == "application/json"
	isOutputJson := r.Header.Get("Accept") == "application/json"

	// get input
	var input proto.Message
	var output proto.Message
	call := func() (retMsg proto.Message, err error) {
		defer func() {
			if e := recover(); e != nil {
				if ee, ok := e.(error); ok {
					err = ee
				} else {
					err = errors.New(fmt.Sprintf("panic %v", e))
				}
				log.Println("panic", err, string(debug.Stack()))
			}
		}()
		var ret interface{}
		ret, err = method.Handler(service.serviceImpl, ctx, func(i interface{}) error {
			var bin []byte
			bin, err = io.ReadAll(r.Body)
			if err != nil {
				return errors.Wrap(err, "read body error")
			}
			input = i.(proto.Message)
			if isInputJson {
				err = protojson.Unmarshal(bin, input)
			} else {
				err = proto.Unmarshal(bin, i.(proto.Message))
				if err != nil {
					return errors.Wrap(err, "unmarshal input error")
				}
			}

			if cb := h.config.OnBeforeCall; cb != nil {
				var newCtx context.Context
				newCtx, err = cb(r, ctx, serviceName, methodName, input)
				if newCtx != nil {
					ctx = newCtx
				}
				if err != nil {
					return errors.Wrap(err, "on before call error")
				}
			}
			return nil
		}, nil)
		if err != nil {
			return nil, err
		}
		return ret.(proto.Message), nil
	}
	ret, err := call()
	output = ret
	if cb := h.config.UnaryCallback; cb != nil {
		cb(ctx, serviceName, methodName, input, output, err)
	}
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	var outputBin []byte
	if isOutputJson {
		w.Header().Set("Content-Type", "application/json")
		outputBin, err = protojson.Marshal(output)
	} else {
		w.Header().Set("Content-Type", "application/protobuf")
		outputBin, err = proto.Marshal(output)
	}
	if err != nil {
		h.writeError(w, errors.Wrap(err, "marshal output error"), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(outputBin)
}

func (h *rpcHttpHandler) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	var methods = make(map[string]*grpc.MethodDesc)
	for i := range desc.Methods {
		d := &desc.Methods[i]
		methods[d.MethodName] = d
	}
	var streams = make(map[string]*grpc.StreamDesc)
	for i := range desc.Streams {
		d := &desc.Streams[i]
		streams[d.StreamName] = d
	}
	h.services[desc.ServiceName] = &serviceInfo{
		serviceImpl: impl,
		methods:     methods,
		streams:     streams,
		mdata:       desc.Metadata,
	}
}
