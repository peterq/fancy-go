package error_code

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"runtime"
)

type withStack struct {
	error
	caller errors.Frame
}

func (w *withStack) StackTrace() errors.StackTrace {
	return errors.StackTrace{w.caller}
}
func (w *withStack) Cause() error { return w.error }

// Unwrap provides compatibility for Go 1.13 error chains.
func (w *withStack) Unwrap() error { return w.error }

func (w *withStack) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			_, _ = fmt.Fprintf(s, "%+v", w.Cause())
			w.caller.Format(s, verb)
			return
		}
		fallthrough
	case 's':
		_, _ = io.WriteString(s, w.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", w.Error())
	}
}

func getCaller() errors.Frame {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	return errors.Frame(pcs[0])
}
