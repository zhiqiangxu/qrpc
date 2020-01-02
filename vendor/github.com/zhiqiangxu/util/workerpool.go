package util

import (
	"errors"
	"runtime"
	"sync"

	"github.com/zhiqiangxu/util/logger"
	"go.uber.org/zap"
)

// WorkerPool is pool of workers
type WorkerPool struct {
	wg       sync.WaitGroup
	doneChan chan struct{}
	workChan chan func()
	once     sync.Once
}

func newWorkerPool() *WorkerPool {
	wp := &WorkerPool{doneChan: make(chan struct{}), workChan: make(chan func())}
	wp.Start()
	return wp
}

// Start worker pool
func (wp *WorkerPool) Start() {
	n := runtime.NumCPU()
	for i := 0; i < n; i++ {
		GoFunc(&wp.wg, func() {
			defer func() {
				err := recover()
				if err != nil {
					logger.Instance().Error("workerPool", zap.Any("err", err))
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

// Close the worker pool
func (wp *WorkerPool) Close() {
	wp.once.Do(func() {
		close(wp.doneChan)
		wp.wg.Wait()
	})
}

var (
	// ErrWorkerPoolClosed when run on closed pool
	ErrWorkerPoolClosed = errors.New("workerPool closed")
)

// Run a task
func (wp *WorkerPool) Run(f func()) error {
	select {
	case wp.workChan <- f:
		return nil
	case <-wp.doneChan:
		return ErrWorkerPoolClosed
	}
}
