package logger

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"
)

var Default = Logger("default")

var (
	loggers     = map[string]Entry{}
	loggersLock sync.Mutex
)

var LogOutput = func(name string) io.Writer {
	lumber := ioutil.Discard
	if os.Getenv("SAVE_LOG") != "" {
		lumber = &lumberjack.Logger{
			Filename:   fmt.Sprintf("%s.log", name),
			MaxSize:    100,
			MaxBackups: 3,
			MaxAge:     10,
		}
	}
	return logToStd{real: lumber}
}

var Hooker = func(name string) Hook {
	return &groupHook{name}
}

type Option func(e *entry)

func OptionOut(out io.Writer) Option {
	return func(e *entry) {
		e.out = out
	}
}
func OptionNoDefaultHooks() Option {
	return func(e *entry) {
		e.hooks = nil
	}
}

func Logger(name string, options ...Option) (l Entry) {
	loggersLock.Lock()
	defer loggersLock.Unlock()
	if l, ok := loggers[name]; ok {
		return l
	}
	l = &entry{
		data:   map[string]interface{}{},
		fields: map[string]interface{}{"type": name},
		out:    LogOutput(name),
		mu:     new(sync.Mutex),
		level:  TraceLevel,
	}
	l.AddHook(Hooker(name))
	loggers[name] = l
	for _, option := range options {
		option(l.(*entry))
	}
	return
}

type logToStd struct {
	real io.Writer
}

func (l logToStd) Write(p []byte) (n int, err error) {
	os.Stderr.Write(p)
	os.Stderr.Write([]byte("\n"))
	return l.real.Write(p)
}

func Notify(topic string, content string, data interface{}) string {
	var id = md5str(fmt.Sprint(time.Now().UnixNano(), content))
	bin, _ := json.Marshal(data)
	Logger("notify").
		WithField("notify_id", id).
		WithField("data", string(bin)).
		WithField("topic", topic).
		Info(content)
	return id
}

func md5str(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

type M = map[string]interface{}
type A = []interface{}
