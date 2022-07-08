package test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime/trace"
	"sync"
	"testing"
	"time"

	"github.com/petermattis/goid"
	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/unix/client"
	"github.com/zhiqiangxu/qrpc/unix/server"
	"github.com/zhiqiangxu/util"
	"gotest.tools/v3/assert"
)

const unixAddr = "/tmp/testunix.sock"

func TestUnixLatency(t *testing.T) {

	os.Remove(unixAddr)
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startUnixServer(ctx)
	})
	time.Sleep(time.Millisecond * 500)

	conf := qrpc.ConnectionConfig{}

	conn, err := client.NewConnection(unixAddr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	assert.Assert(t, err == nil)

	fmt.Println("|TestUnixLatency goid|", goid.Get())

	payload := bytes.Repeat([]byte("xu"), 200)
	starttrace := time.Now()
	f, err := os.Create(time.Now().Format("unix.out"))
	if err != nil {
		panic(err)
	}
	trace.Start(f)

	for i := 0; i < 1; i++ {
		start := time.Now()
		_, resp, err := conn.Request(HelloCmd, 0, payload)

		assert.Assert(t, err == nil)

		resp.GetFrame()

		fmt.Println("single request took", time.Since(start), "startts", start.Second(), start.Nanosecond())

		fmt.Println("-------------")
	}

	fmt.Println("time after trace", time.Since(starttrace))
	trace.Stop()

	cancelFunc()
	wg.Wait()
}

func startUnixServer(ctx context.Context) {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		// time.Sleep(time.Hour)
		writer.StartWrite(request.RequestID, HelloRespCmd, 0)

		writer.WriteBytes(append([]byte("hello world "), request.Payload...))
		err := writer.EndWrite()
		if err != nil {
			panic(err)
		}
	})
	subFunc := func(ci *qrpc.ConnectionInfo, request *qrpc.Frame) {
		fmt.Println("pushedmsg")
	}
	bindings := []qrpc.ServerBinding{
		{Addr: unixAddr, Handler: handler, SubFunc: subFunc, ReadFrameChSize: 10000, WriteFrameChSize: 1000}}

	server := server.New(bindings)

	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})

}
