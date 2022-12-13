package error_code

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
)

type CodedError interface {
	error
	Code() int
	Wrap(err error) CodedError
	CodeEqual(err error) bool
	WrapWith(err error, s string) CodedError
	WithExtra(key string, value interface{}) CodedError
}

type withCode struct {
	code  int
	extra map[string]interface{}
	error
}

func (n *withCode) WithExtra(key string, value interface{}) CodedError {
	extra := copyExtra(n.extra)
	extra[key] = value
	return &withCode{
		code:  n.code,
		extra: extra,
		error: n.error,
	}
}

func (n *withCode) Extra() map[string]interface{} {
	return n.extra
}

func (n *withCode) CodeEqual(err error) bool {
	return GetCode(err) == n.Code()
}

func (n *withCode) Cause() error { return n.error }

// Unwrap provides compatibility for Go 1.13 error chains.
func (n *withCode) Unwrap() error { return n.error }

func (n *withCode) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			_, _ = fmt.Fprintf(s, "%+v", n.Cause())
			return
		}
		fallthrough
	case 's':
		_, _ = io.WriteString(s, n.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", n.Error())
	}
}

func (n *withCode) Wrap(err error) CodedError {
	if err == nil {
		return nil
	}
	return &withCode{
		code:  n.code,
		extra: copyExtra(n.extra),
		error: &withStack{caller: getCaller(), error: errors.WithMessage(err, n.Error())},
	}
}

func (n *withCode) WrapWith(err error, s string) CodedError {
	if err == nil {
		return nil
	}
	return &withCode{
		code:  n.code,
		extra: copyExtra(n.extra),
		error: &withStack{caller: getCaller(), error: errors.WithMessage(errors.WithMessage(err, n.Error()), s)},
	}
}

func (n *withCode) Code() int {
	return n.code
}

func NewError(message string, code int) CodedError {
	return &withCode{
		code:  code,
		error: &withStack{caller: getCaller(), error: errors.New(message)},
	}
}

func From(err error) CodedError {
	if err == nil {
		return nil
	}
	if e, ok := err.(CodedError); ok {
		return e
	}
	return &withCode{
		code:  GetCode(err),
		error: err,
	}
}

var codeMap = map[int]string{}

func define(code int, message string) CodedError {
	if _, ok := codeMap[code]; ok {
		panic(errors.New(fmt.Sprintf("duplicate error code %d; %s", code, message)))
	}
	codeMap[code] = message
	return NewError(message, code)
}

func Define(code int, message string) CodedError {
	return define(code, message)
}

func ToResponse(err error, detail bool) (int, string) {
	if err == nil {
		return 0, "ok"
	}
	code := From(err).Code()
	if detail {
		return code, err.Error()
	}
	message := err.Error()
	return code, message
}

func GetCode(err error) int {
	if err == nil {
		return 0
	}
	for err != nil {
		if codeErr, ok := err.(interface{ Code() int }); ok {
			if codeErr.Code() != -1 {
				return codeErr.Code()
			}
		}
		cause, ok := err.(interface{ Cause() error })
		if !ok {
			break
		}
		err = cause.Cause()
	}
	return -1
}

func copyExtra(extra map[string]interface{}) map[string]interface{} {
	var newOne = map[string]interface{}{}
	for k, v := range extra {
		newOne[k] = v
	}
	return newOne
}
