package qrpc

import (
	"sync"
)

// GoFunc runs a goroutine under WaitGroup
func GoFunc(routinesGroup *sync.WaitGroup, f func()) {
	routinesGroup.Add(1)
	go func() {
		defer routinesGroup.Done()
		f()
	}()
}
