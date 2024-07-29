package app

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	exitHooks   []func(ctx context.Context)
	ctx, cancel = context.WithCancel(context.Background())
	taskWg      sync.WaitGroup
)

func OnExit(fn func(ctx context.Context)) {
	exitHooks = append(exitHooks, fn)
}

func Context() context.Context {
	return ctx
}

func WithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}

func WithTimeout(du time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, du)
}

func Done() bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func TaskStart() func() {
	var once sync.Once
	taskWg.Add(1)
	return func() {
		once.Do(func() {
			taskWg.Done()
		})
	}
}

var exitOnce sync.Once

var exitErr error

func ExitWithErr(err error) {
	println("exiting... caused by error: ", err.Error())
	exitOnce.Do(func() {
		exitErr = err
		cancel()
		doExit()
	})
}

func GetExitErr() error {
	return exitErr
}

func Exit() {
	println("exiting... caused by manual exit call")
	exitOnce.Do(func() {
		cancel()
		doExit()
	})
}

func init() {
	shutDownCh := make(chan os.Signal, 3)
	signal.Notify(shutDownCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-shutDownCh
		ExitWithErr(errors.New(fmt.Sprintf("os signal: %s", sig.String())))
	}()
}

func doExit() {
	timeout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var hookWg sync.WaitGroup
	hookWg.Add(len(exitHooks))
	for _, hook := range exitHooks {
		go func(hook func(ctx context.Context)) {
			defer hookWg.Done()
			hook(timeout)
		}(hook)
	}

	var hookWgDone = waitWg(&hookWg)
	var taskWgDone = waitWg(&taskWg)

	select {
	case <-timeout.Done():
		println("tasks finished timeout")
		os.Exit(-1)
	case <-taskWgDone:
		println("all tasks finished")
	}

	select {
	case <-timeout.Done():
		println("exit hooks timeout")
		os.Exit(-1)
	case <-hookWgDone:
		println("all exit hooks returned, exit now")
		os.Exit(0)
	}
}

func waitWg(wg *sync.WaitGroup) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}
