package ws_agent

import (
	"encoding/json"
	"golang.org/x/net/websocket"
	"log"
	"net/http"
	"reflect"
	"sync"
)

func NewLogSubPoint() (http.HandlerFunc, func(data interface{})) {
	var logChannels []func([]byte)
	var logChannelsMu sync.Mutex
	logToClient := func(data interface{}) {
		bin, err := json.Marshal(data)
		if err != nil {
			log.Println("marshal json", err)
			return
		}
		cs := make([]func([]byte), len(logChannels))
		copy(cs, logChannels)
		for _, c := range cs {
			c(bin)
		}
	}
	removeCb := func(cb func([]byte)) {
		logChannelsMu.Lock()
		defer logChannelsMu.Unlock()
		idx := -1
		thisPtr := reflect.ValueOf(cb).Pointer()
		for i, fn := range logChannels {
			if reflect.ValueOf(fn).Pointer() == thisPtr {
				idx = i
				break
			}
		}
		if idx >= 0 {
			if idx == len(logChannels)-1 {
				logChannels = logChannels[:idx]
			} else {
				logChannels = append(logChannels[:idx], logChannels[idx+1:]...)
			}
		}
	}

	logWs := websocket.Server{
		Handler: func(conn *websocket.Conn) {
			var cb func([]byte)
			signal := make(chan struct{})
			cb = func(bin []byte) {
				_, err := conn.Write(bin)
				if err != nil {
					log.Println("write to log client", err)
					removeCb(cb)
					close(signal)
					return
				}

			}
			logChannelsMu.Lock()
			logChannels = append(logChannels, cb)
			logChannelsMu.Unlock()
			select {
			case <-signal:
				return
			case <-conn.Request().Context().Done():
				removeCb(cb)
				log.Println("log client close")
				return
			}
		},
	}
	return logWs.ServeHTTP, logToClient
}
