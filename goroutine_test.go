package qrpc

import (
	"testing"
	"sync"
	"fmt"
)

func TestGoFunc_panic(t *testing.T)  {
	 wg :=sync.WaitGroup{}
	GoFunc(&wg, func() {
		panicFunc()
	})
}

func panicFunc()  {
	list:=[]string{"0","1"}
	fmt.Println(list[10])
}