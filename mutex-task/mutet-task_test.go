package mutex_task

import (
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMutexTask(t *testing.T) {
	var cnt int64
	task := NewMutexTask(func(p string) (string, error) {
		log.Println(atomic.AddInt64(&cnt, 1))
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
		return p, nil
	}, func(p string) string {
		return p
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				task.Exec("hello")
			}
		}()
	}
	wg.Wait()
}
