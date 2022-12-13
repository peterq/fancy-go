package error_code

import (
	"fmt"
	"github.com/pkg/errors"
	"runtime"
	"strings"
)

func ToLog(err error) (string, []string, map[string]interface{}) {
	if err == nil {
		return "", nil, nil
	}
	var stack []string
	var content = err.Error()
	type causer interface {
		Cause() error
	}
	type stackTracer interface {
		causer
		StackTrace() errors.StackTrace
	}
	type extra interface {
		Extra() map[string]interface{}
	}
	var extraData = map[string]interface{}{}
	var idx int
	for err != nil {
		idx++
		st, ok := err.(stackTracer)
		if ok {
			msg := err.Error()
			if causer, ok := st.Cause().(causer); ok {
				msg = strings.Replace(msg, causer.Cause().Error(), "", 1)
				msg = strings.TrimRight(msg, ": ")
			}
			trace := st.StackTrace()
			r := runtime.FuncForPC(uintptr(trace[0]) - 1)
			_, ln := r.FileLine(uintptr(trace[0]) - 1)
			fn := strings.Replace(r.Name(), "github.com/1second/", "", 1)
			stack = append(stack, fmt.Sprintf("[%s:%d] %s", fn, ln, msg))
		}

		if ex, ok := err.(extra); ok {
			if extra := ex.Extra(); extra != nil {
				for k, v := range extra {
					if _, ok := extraData[k]; ok {
						extraData[fmt.Sprint(k, ".", idx)] = v
					} else {
						extraData[k] = v
					}
				}
			}
		}
		cause, ok := err.(causer)
		if !ok {
			break
		}
		err = cause.Cause()
	}

	return content, stack, extraData
}
