package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/peterq/fancy-go/error-code"
	"io"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Entry interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Print(args ...interface{})
	Warn(args ...interface{})
	Warning(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})
	Panic(args ...interface{})
	WithField(key string, value interface{}) Entry

	WithType(name interface{}) Entry
	WithObjectId(id interface{}) Entry
	WithStage(stage string) Entry
	WithData(key string, value interface{}) Entry
	WithDataMap(mp map[string]interface{}) Entry
	ErrorOrInfo(err error, msg string) error
	AddHook(hook Hook)
	SetStackSkip(skip int) Entry
	SetLevel(level Level) Entry
}

type entry struct {
	data      map[string]interface{}
	fields    map[string]interface{}
	hooks     []Hook
	stackSkip int
	level     Level

	out io.Writer
	mu  *sync.Mutex
}

func (e *entry) logLevel(stackSkip int, level Level, args ...interface{}) {
	if level > e.level {
		return
	}
	stackSkip += e.stackSkip
	pc := getCaller(stackSkip)
	r := runtime.FuncForPC(pc)
	file, ln := r.FileLine(pc - 1)
	msg := fmt.Sprintln(args...)
	msg = msg[:len(msg)-1]

	buf := bytes.NewBufferString(fmt.Sprint(time.Now().Format("2006-01-02T15:04:05.000"), " [", level.String(), "]"))
	buf.WriteString(fmt.Sprintf(" %s:%d", path.Join(path.Base(file)), ln))
	//buf.WriteString( fmt.Sprintf(" [%s]", r.Name()))
	fields := e.fields
	if len(e.data) > 0 {
		fields["data"] = e.data
	}

	buf.WriteRune(' ')
	buf.WriteString(msg)
	bin, err := json.Marshal(fields)
	if err != nil {
		bin = []byte(fmt.Sprintf("$logger-error$ marshal fields: %s: %#v", err.Error(), fields))
	}
	buf.WriteRune(' ')
	buf.Write(bin)
	buf.WriteRune('\n')

	e.fireRecord(msg, level, file, ln, r)

	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = buf.WriteTo(e.out)
}

func (e *entry) fireRecord(msg string, level Level, file string, ln int, caller *runtime.Func) {
	r := &Record{
		Data:     e.data,
		Fields:   e.fields,
		Message:  msg,
		Level:    level,
		Caller:   caller,
		Location: fmt.Sprintf("%s:%d", file, ln),
	}
	for _, hook := range e.hooks {
		hook.Fire(r)
	}
}

func (e *entry) AddHook(hook Hook) {
	e.hooks = append(e.hooks, hook)
}

func (e *entry) Debug(args ...interface{}) {
	e.logLevel(1, DebugLevel, args...)
}

func (e *entry) Info(args ...interface{}) {
	e.logLevel(1, InfoLevel, args...)
}

func (e *entry) Print(args ...interface{}) {
	e.logLevel(1, InfoLevel, args...)
}

func (e *entry) Warn(args ...interface{}) {
	e.logLevel(1, WarnLevel, args...)
}

func (e *entry) Warning(args ...interface{}) {
	e.logLevel(1, WarnLevel, args...)
}

func (e *entry) Fatal(args ...interface{}) {
	e.logLevel(1, FatalLevel, args...)
}

func (e *entry) Panic(args ...interface{}) {
	e.logLevel(1, PanicLevel, args...)
}

var _ Entry = (*entry)(nil)

func copyData(old map[string]interface{}) map[string]interface{} {
	copied := map[string]interface{}{}
	for k, v := range old {
		copied[k] = v
	}
	return copied
}

func (e *entry) clone() *entry {
	var newEntry = new(entry)
	*newEntry = *e
	newEntry.data = copyData(e.data)
	newEntry.fields = copyData(e.fields)
	newEntry.hooks = make([]Hook, len(e.hooks), cap(e.hooks))
	copy(newEntry.hooks, e.hooks)
	return newEntry
}

func (e *entry) WithData(key string, value interface{}) Entry {
	return e.withData(key, value)
}

func (e *entry) WithDataMap(mp map[string]interface{}) Entry {
	return e.withDataMap(mp)
}

func (e *entry) WithField(key string, value interface{}) Entry {
	return e.withField(key, value)
}

func (e *entry) withField(name string, value interface{}) *entry {
	newEntry := e.clone()
	newEntry.fields[name] = value
	return newEntry
}

func (e *entry) withData(key string, value interface{}) *entry {
	newEntry := e.clone()
	newEntry.data[key] = value
	return newEntry
}

func (e *entry) withDataMap(mp map[string]interface{}) *entry {
	newEntry := e.clone()
	for k, v := range mp {
		newEntry.data[k] = v
	}
	return newEntry
}

func (e *entry) logErr(err error) {
	msg, stack, extra := error_code.ToLog(err)
	var entry = e
	if len(extra) > 0 {
		entry = e.withData("extra", extra)
	}
	entry.withField("error_code", error_code.GetCode(err)).
		withField("error_stack", strings.Join(stack, "\n")).logLevel(2, ErrorLevel, msg)
	return
}

func (e *entry) WithType(typ interface{}) Entry {
	return e.withField("type", typ)
}

func (e *entry) WithObjectId(id interface{}) Entry {
	return e.withField("id", id)
}

func (e *entry) SetStackSkip(skip int) Entry {
	newE := e.clone()
	newE.stackSkip = skip
	return newE
}

func (e *entry) SetLevel(level Level) Entry {
	newE := e.clone()
	newE.level = level
	return newE
}

func (e *entry) WithStage(stage string) Entry {
	if stage == "" {
		return e.withField("stage", stage)
	}
	if old, ok := e.fields["stage"].(string); ok {
		if len(old) > 500 {
			old = "$stage-too-long$"
		}
		stage = old + "." + stage
	}
	return e.withField("stage", stage)
}

func (e *entry) Error(args ...interface{}) {
	if len(args) == 1 {
		if args[0] == nil {
			return
		}
		if err, ok := args[0].(error); ok {
			e.logErr(err)
			return
		}
	}
	e.logLevel(2, ErrorLevel, args...)
}

func (e *entry) ErrorOrInfo(err error, msg string) error {
	if err != nil {
		e.logErr(err)
	} else {
		e.logLevel(1, InfoLevel, msg)
	}
	return err
}
