package qrpc

import (
	"errors"
	"runtime"
	"sync"

	"go.uber.org/zap"
)

type workerPool struct {
	wg       sync.WaitGroup
	doneChan chan struct{}
	workChan chan func()
}

func newWorkerPool() *workerPool {
	wp := &workerPool{doneChan: make(chan struct{}), workChan: make(chan func())}
	wp.start()
	return wp
}

func (wp *workerPool) start() {
	n := runtime.NumCPU()
	for i := 0; i < n; i++ {
		GoFunc(&wp.wg, func() {
			defer func() {
				err := recover()
				if err != nil {
					l.Error("workerPool", zap.Any("err", err))
				}
			}()
			for {
				select {
				case f := <-wp.workChan:
					f()
				case <-wp.doneChan:
					return
				}
			}
		})
	}
}

func (wp *workerPool) close() {
	close(wp.doneChan)
	wp.wg.Wait()
}

var (
	errWorkerPoolClosed = errors.New("workerPool closed")
)

func (wp *workerPool) run(f func()) error {
	select {
	case wp.workChan <- f:
		return nil
	case <-wp.doneChan:
		return errWorkerPoolClosed
	}
}
