package logger

import (
	"fmt"
	"runtime"
	"strings"
)

type Level uint32

func (level Level) String() string {
	if b, err := level.MarshalText(); err == nil {
		return string(b)
	} else {
		return "unknown"
	}
}

func ParseLevel(lvl string) (Level, error) {
	switch strings.ToLower(lvl) {
	case "panic":
		return PanicLevel, nil
	case "fatal":
		return FatalLevel, nil
	case "error":
		return ErrorLevel, nil
	case "warn", "warning":
		return WarnLevel, nil
	case "info":
		return InfoLevel, nil
	case "debug":
		return DebugLevel, nil
	case "trace":
		return TraceLevel, nil
	}

	var l Level
	return l, fmt.Errorf("not a valid Level: %q", lvl)
}

// A constant exposing all logging levels
//var AllLevels = []Level{
//	PanicLevel,
//	FatalLevel,
//	ErrorLevel,
//	WarnLevel,
//	InfoLevel,
//	DebugLevel,
//	TraceLevel,
//}

const (
	PanicLevel Level = iota
	FatalLevel
	ErrorLevel
	WarnLevel
	InfoLevel
	DebugLevel
	TraceLevel
)

// UnmarshalText implements encoding.TextUnmarshaler.
func (level *Level) UnmarshalText(text []byte) error {
	l, err := ParseLevel(string(text))
	if err != nil {
		return err
	}

	*level = l

	return nil
}

func (level Level) MarshalText() ([]byte, error) {
	switch level {
	case TraceLevel:
		return []byte("trace"), nil
	case DebugLevel:
		return []byte("debug"), nil
	case InfoLevel:
		return []byte("info"), nil
	case WarnLevel:
		return []byte("warning"), nil
	case ErrorLevel:
		return []byte("error"), nil
	case FatalLevel:
		return []byte("fatal"), nil
	case PanicLevel:
		return []byte("panic"), nil
	}

	return nil, fmt.Errorf("not a valid level %d", level)
}

type Hook interface {
	Fire(record *Record)
}

type Record struct {
	Data     map[string]interface{}
	Fields   map[string]interface{}
	Message  string
	Level    Level
	Caller   *runtime.Func
	Location string
}

func getCaller(skip int) uintptr {
	var pcs [1]uintptr
	runtime.Callers(3+skip, pcs[:])
	return pcs[0]
}
