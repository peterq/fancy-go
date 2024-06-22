package err_util

import "errors"

type Retryable interface {
	Retryable() bool
}

func IsRetryable(err error, def bool) bool {
	var r Retryable
	if errors.As(err, &r) {
		return r.Retryable()
	}
	return def
}

type retryable struct {
	canRetry bool
	error
}

func SetRetryable(err error, on bool) error {
	if err == nil {
		return nil
	}
	return &retryable{
		canRetry: on,
		error:    err,
	}
}

func (r *retryable) Retryable() bool {
	return r.canRetry
}

func (r *retryable) Unwrap() error {
	return r.error
}
