package test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/ws/client"
	"github.com/zhiqiangxu/qrpc/ws/server"
	"github.com/zhiqiangxu/util"
)

func TestWSOverlay(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServerForWSOverlay(ctx)
	})
	time.Sleep(time.Millisecond * 300)

	conf := qrpc.ConnectionConfig{}

	conn, err := client.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
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

func startServerForWSOverlay(ctx context.Context) {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		writer.StartWrite(request.RequestID, HelloRespCmd, 0)

		writer.WriteBytes(append([]byte("hello world "), request.Payload...))
		err := writer.EndWrite()
		if err != nil {
			fmt.Println("EndWrite", err)
		}
	})

	subFunc := func(ci *qrpc.ConnectionInfo, request *qrpc.Frame) {
		fmt.Println("pushedmsg")
	}
	bindings := []qrpc.ServerBinding{
		{Addr: addr, Handler: handler, SubFunc: subFunc, ReadFrameChSize: 10000, WriteFrameChSize: 1000, WBufSize: 2000000, RBufSize: 2000000}}
	server := server.New(bindings)
	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})
}

func TestPerformanceWS(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServerForWSOverlay(ctx)
	})
	time.Sleep(time.Second)
	conn, err := client.NewConnection(addr, qrpc.ConnectionConfig{WriteFrameChSize: 1000}, nil)
	if err != nil {
		panic(err)
	}
	i := 0
	{
		var wg sync.WaitGroup
		startTime := time.Now()
		for {

			util.GoFunc(&wg, func() {
				_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
				if err != nil {
					panic(err)
				}
				frame, err := resp.GetFrame()
				if err != nil || !bytes.Equal(frame.Payload, []byte("hello world xu")) {
					panic(fmt.Sprintf("fail payload:%s len:%v cmd:%v flags:%v err:%v", string(frame.Payload), len(frame.Payload), frame.Cmd, frame.Flags, err))
				}
			})
			i++
			if i > n {
				break
			}
		}
		wg.Wait()
		conn.Close()
		endTime := time.Now()

		t.Log(n, "request took", endTime.Sub(startTime))
	}

	cancelFunc()
	wg.Wait()
}
