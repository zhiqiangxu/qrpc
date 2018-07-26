package test

import (
	"fmt"
	"testing"
	"time"

	"github.com/zhiqiangxu/qrpc"
)

const (
	addr = "0.0.0.0:8080"
)

// TestConnection tests connection
func TestHelloWorld(t *testing.T) {

	go startServer()
	time.Sleep(time.Second * 2)

	conf := qrpc.ConnectionConfig{}
	cli := qrpc.NewClient(conf)

	conn := cli.GetConn(addr, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})

	for _, flag := range []qrpc.FrameFlag{0, qrpc.NBFlag} {
		_, resp, err := conn.Request(HelloCmd, flag, []byte("xu"))
		if err != nil {
			panic(err)
		}
		frame := resp.GetFrame()
		if frame == nil {
			panic("nil frame")
		}
		fmt.Println("resp is ", string(frame.Payload))
	}

}

func TestWriter(t *testing.T) {

	go startServer()
	time.Sleep(time.Second * 2)

	conf := qrpc.ConnectionConfig{WriteTimeout: 2}
	cli := qrpc.NewClient(conf)

	conn := cli.GetConn(addr, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})

	w := conn.GetWriter()
	for i := 0; ; i++ {
		fmt.Println(i)
		w.StartWrite(uint64(i), HelloCmd, qrpc.NBFlag)
		w.WriteBytes([]byte("TestWriter"))
		err := w.EndWrite()
		if err != nil {
			panic(err)
		}
	}

}

func TestCancel(t *testing.T) {

	go startServerForCancel()
	time.Sleep(time.Second * 2)

	conf := qrpc.ConnectionConfig{}
	cli := qrpc.NewClient(conf)

	conn := cli.GetConn(addr, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})

	requestID, resp, err := conn.Request(HelloCmd, 0, []byte("xu"))

	err = conn.GetWriter().ResetFrame(requestID, 0)
	if err != nil {
		panic(err)
	}
	frame := resp.GetFrame()
	if frame == nil {
		panic("nil frame")
	}
	fmt.Println("resp is ", string(frame.Payload))

}

const (
	HelloCmd qrpc.Cmd = iota
	HelloRespCmd
)

func startServer() {
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
	bindings := []qrpc.ServerBinding{
		qrpc.ServerBinding{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)
	err := server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func startServerForCancel() {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		// time.Sleep(time.Hour)
		select {
		case <-request.Context().Done():
			writer.StartWrite(request.RequestID, HelloRespCmd, 0)

			writer.WriteBytes(append([]byte("hello canceled "), request.Payload...))
			err := writer.EndWrite()
			if err != nil {
				fmt.Println("EndWrite", err)
			}
		}
	})
	bindings := []qrpc.ServerBinding{
		qrpc.ServerBinding{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)
	err := server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
