package websocket_util

import (
	"context"
	"github.com/pkg/errors"
	"golang.org/x/net/websocket"
	"io"
	"log"
)

// ReceiveFullFrame 接受完整帧
func ReceiveFullFrame(ws *websocket.Conn, ctx context.Context) (byte, []byte, error) {
	return ReceiveFullFramePingPongCb(ws, ctx, nil)
}

// ReceiveFullFramePingPongCb 接受完整帧
func ReceiveFullFramePingPongCb(ws *websocket.Conn, ctx context.Context, pingPongCallback func(byte, []byte)) (byte, []byte, error) {
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

type WritePong interface {
	WritePong(msg []byte) (n int, err error)
}

// 接受帧
func receiveFrame(cd websocket.Codec, ws *websocket.Conn, v interface{}, ctx context.Context, pingPongCb func(byte2 byte, data []byte)) (payloadType byte, fin bool, err error) {
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
		b := make([]byte, 1024)
		var n int
		n, err = io.ReadFull(frame, b)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, false, errors.Wrap(err, "read ping pong error")
		}
		_, _ = io.Copy(io.Discard, frame)
		if frame.PayloadType() == websocket.PingFrame {
			var handler interface{} = ws
			if _, err := handler.(WritePong).WritePong(b[:n]); err != nil {
				return 0, false, errors.Wrap(err, "write pong error")
			}
		}
		if pingPongCb != nil {
			pingPongCb(frame.PayloadType(), b[:n])
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

	data, err := io.ReadAll(frame)
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
