package mutex_task

import (
	"fmt"
	"github.com/pkg/errors"
	"sync"
)

type MutexTask[P any, R any] interface {
	Exec(P) (R, error)
}

func NewMutexTask[P any, R any](exec func(P) (R, error), keyGetter func(P) string) MutexTask[P, R] {
	return &mutexTask[P, R]{
		exec:      exec,
		keyGetter: keyGetter,
		mu:        sync.Mutex{},
		waitingMap: map[string][]chan struct {
			result R
			err    error
		}{},
	}
}

type mutexTask[P any, R any] struct {
	exec      func(P) (R, error)
	keyGetter func(P) string

	mu         sync.Mutex
	waitingMap map[string][]chan struct {
		result R
		err    error
	}
}

func (m mutexTask[P, R]) Exec(p P) (R, error) {
	key := m.keyGetter(p)
	m.mu.Lock()
	if _, ok := m.waitingMap[key]; !ok { // not running
		m.waitingMap[key] = []chan struct {
			result R
			err    error
		}{}
		m.mu.Unlock()
	} else {
		ch := make(chan struct {
			result R
			err    error
		}, 1)
		m.waitingMap[key] = append(m.waitingMap[key], ch)
		m.mu.Unlock()
		result := <-ch
		return result.result, result.err
	}

	var result R
	var err error
	defer func() {
		if e := recover(); e != nil {
			var ok bool
			if err, ok = e.(error); !ok {
				err = errors.New(fmt.Sprintf("panic: %v", e))
			} else {
				err = errors.WithStack(err)
			}
		}
		m.mu.Lock()
		waitingList := m.waitingMap[key]
		delete(m.waitingMap, key)
		m.mu.Unlock()
		for _, ch := range waitingList {
			ch <- struct {
				result R
				err    error
			}{result: result, err: err}
		}
	}()
	result, err = m.exec(p)
	return result, err
}
