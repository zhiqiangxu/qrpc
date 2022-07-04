package test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/kcp/client"
	"github.com/zhiqiangxu/qrpc/kcp/server"
	"github.com/zhiqiangxu/util"
)

const (
	kcpAddr = "localhost:8001"
)

func TestKCP(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServerForKCP(ctx)
	})
	time.Sleep(time.Millisecond * 300)

	conf := qrpc.ConnectionConfig{}

	conn, err := client.NewConnection(quicAddr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println("pushed", frame)
	})
	if err != nil {
		panic(err)
	}

	_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
	if err != nil {
		panic(err)
	}

	frame, err := resp.GetFrame()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(frame.Payload))

	cancelFunc()
	wg.Wait()
}

func startServerForKCP(ctx context.Context) {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		writer.StartWrite(request.RequestID, HelloRespCmd, 0)

		writer.WriteBytes(append([]byte("hello world for kcp"), request.Payload...))
		err := writer.EndWrite()
		if err != nil {
			fmt.Println("EndWrite", err)
		}
	})
	bindings := []qrpc.ServerBinding{
		{Addr: kcpAddr, Handler: handler}}
	server := server.New(bindings)
	util.RunWithCancel(ctx, func() {
		err := server.ListenAndServe()
		if err != nil {
			fmt.Println("ListenAndServe", err)
		}
	}, func() {
		server.Shutdown()
	})
}
