// 这个包是 sync.cond 的channel实现方式, 旨在解决 sync.cond 不能和其他通道多路监听

package cond_chan

import (
	"sync"
)

type Cond interface {
	Wait() <-chan bool
	Signal()
	Broadcast()
}

var _ Cond = (*cond)(nil)

func NewCond() Cond {
	return &cond{
		ch: make(chan bool, 1),
	}
}

type cond struct {
	mu sync.RWMutex
	ch chan bool
}

func (c *cond) Wait() <-chan bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ch
}

func (c *cond) Signal() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	select {
	case c.ch <- true:
	default:
	}
}

func (c *cond) Broadcast() {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.ch)
	c.ch = make(chan bool)
}
