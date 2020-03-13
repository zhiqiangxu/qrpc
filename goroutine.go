package qrpc

import (
	"sync"
	"runtime"

	"go.uber.org/zap"
)

// GoFunc runs a goroutine under WaitGroup
func GoFunc(routinesGroup *sync.WaitGroup, f func()) {
	routinesGroup.Add(1)
	_, file, line, _ := runtime.Caller(1)
	go func() {
		defer func() {
			if err := recover();err != nil {
				l.Error("GoFunc panic", zap.String("file", file), zap.Int("line", line))
			}
		}()
		defer routinesGroup.Done()
		f()
	}()
}
