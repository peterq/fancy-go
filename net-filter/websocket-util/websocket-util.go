package websocket_util

import (
	"context"
	"golang.org/x/net/websocket"
	"io/ioutil"
	"log"
)

// ReceiveFullFrame 接受完整帧
func ReceiveFullFrame(ws *websocket.Conn, ctx context.Context) (byte, []byte, error) {
	return ReceiveFullFramePingPongCb(ws, ctx, nil)
}

// ReceiveFullFramePingPongCb 接受完整帧
func ReceiveFullFramePingPongCb(ws *websocket.Conn, ctx context.Context, pingPongCallback func(byte)) (byte, []byte, error) {
	var data []byte
	var pType byte
	for {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		var seg []byte
		payloadType, fin, err := receiveFrame(websocket.Message, ws, &seg, ctx, pingPongCallback)
		if err != nil {
			return 0, nil, err
		}
		data = append(data, seg...)
		if fin {
			pType = payloadType
			break
		}
	}
	return pType, data, nil
}

// 接受帧
func receiveFrame(cd websocket.Codec, ws *websocket.Conn, v interface{}, ctx context.Context, pingPongCb func(byte2 byte)) (payloadType byte, fin bool, err error) {
again:
	if ctx.Err() != nil {
		return payloadType, fin, ctx.Err()
	}
	frame, err := ws.NewFrameReader()
	if frame.HeaderReader() != nil {
		bin := make([]byte, 1)
		_, _ = frame.HeaderReader().Read(bin)
		fin = ((bin[0] >> 7) & 1) != 0
	}
	if err != nil {
		return
	}
	if frame.PayloadType() == websocket.PingFrame || frame.PayloadType() == websocket.PongFrame {
		if pingPongCb != nil {
			pingPongCb(frame.PayloadType())
		}
	}
	frame, err = ws.HandleFrame(frame)
	if err != nil {
		return
	}
	if frame == nil {
		goto again
	}

	payloadType = frame.PayloadType()

	data, err := ioutil.ReadAll(frame)
	if err != nil {
		return
	}
	return payloadType, fin, cd.Unmarshal(data, payloadType, v)
}

type frameIo struct {
	ws *websocket.Conn
}

func (f *frameIo) WriteCtx(payloadType byte, bin []byte, ctx context.Context) error {
	f.ws.PayloadType = payloadType
	l, err := f.ws.Write(bin)
	log.Println("write", l, err, len(bin))
	return err
}

func (f *frameIo) ReadCtx(ctx context.Context) (byte, []byte, error) {
	return ReceiveFullFrame(f.ws, ctx)
}

type FrameIo interface {
	WriteCtx(payloadType byte, bin []byte, ctx context.Context) error
	ReadCtx(ctx context.Context) (byte, []byte, error)
}

func NewFrameIo(ws *websocket.Conn) FrameIo {
	return &frameIo{ws: ws}
}
