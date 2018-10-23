package qrpc

import (
	"sync"

	"github.com/panjf2000/ants"
)

// GoFunc runs a goroutine under WaitGroup
func GoFunc(routinesGroup *sync.WaitGroup, f func()) {
	routinesGroup.Add(1)
	err := ants.Submit(func() error {
		defer routinesGroup.Done()
		f()
		return nil
	})
	if err != nil {
		logError("ants Submit", err)
	}
}
