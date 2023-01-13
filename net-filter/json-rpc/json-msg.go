package json_rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/peterq/fancy-go/utils/json-util"
	"github.com/pkg/errors"
	"io"
	"regexp"
	"strconv"
	"sync"
)

type JsonMessageChannel interface {
	ReadCtx(ctx context.Context, msg *Message) error
	WriteCtx(ctx context.Context, msg *Message) error
	Close() error
	Closed() bool
}

type ChannelWithBrokenSignal interface {
	BrokenSignal() <-chan bool
	WriteCtxWithBrokenSignal(ctx context.Context, msg *Message) (<-chan bool, error)
}

type lengthPrefixedChannel struct {
	readMutex  sync.Mutex
	writeMutex sync.Mutex
	conn       io.ReadWriteCloser
	scanner    *bufio.Scanner
	closed     func() bool
}

var jsonLengthRe = regexp.MustCompile(`^\s*(\d+),`)

func SplitJson(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, bufio.ErrFinalToken
	}
	parts := jsonLengthRe.FindSubmatch(data)
	if len(parts) != 2 {
		if len(data) > 10 {
			return 0, nil, bufio.ErrTooLong
		}
		return 0, nil, nil
	}
	var length int64
	length, err = strconv.ParseInt(string(parts[1]), 10, 64)
	if err != nil {
		return 0, nil, errors.Wrap(err, "invalid json length prefix")
	}

	if length > 1024*1024*2 {
		return 0, nil, errors.New("too large, max 2MB")
	}
	headerLen := len(parts[0])
	if int64(len(data[headerLen:])) < length {
		return 0, nil, nil
	}
	//log.Println(headerLen+int(length), utils.JsonRet(string(data[headerLen:headerLen+int(length)])))
	return headerLen + int(length), data[headerLen : headerLen+int(length)], nil

}

func (l *lengthPrefixedChannel) ReadCtx(ctx context.Context, msg *Message) error {
	l.readMutex.Lock()
	defer l.readMutex.Unlock()
	if !l.scanner.Scan() {
		return l.scanner.Err()
	}
	*msg = Message{}
	err := json.Unmarshal(l.scanner.Bytes(), msg)
	if err != nil {
		return errors.Wrap(err, "invalid json message")
	}
	return nil
}

func (l *lengthPrefixedChannel) WriteCtx(ctx context.Context, msg *Message) error {
	bin := json_util.ToBin(msg)
	bin = append([]byte(fmt.Sprintf("%d,", len(bin))), bin...)
	l.writeMutex.Lock()
	defer l.writeMutex.Unlock()
	const segmentSize = 1024
	for len(bin) > 0 {
		var n int
		var err error
		if len(bin) > segmentSize {
			n, err = l.conn.Write(bin[:segmentSize])
		} else {
			n, err = l.conn.Write(bin)
		}
		if err != nil {
			return err
		}
		bin = bin[n:]
	}
	_, err := l.conn.Write(bin)
	return err
}

func (l *lengthPrefixedChannel) Close() error {
	return l.conn.Close()
}

func (l *lengthPrefixedChannel) Closed() bool {
	return l.closed()
}

func ReadWriteCloserToLengthPrefixedChannel(conn io.ReadWriteCloser, closed func() bool) JsonMessageChannel {
	scanner := bufio.NewScanner(conn)
	scanner.Split(SplitJson)
	return &lengthPrefixedChannel{
		conn:    conn,
		scanner: scanner,
		closed:  closed,
	}
}
