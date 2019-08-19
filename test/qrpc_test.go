package test

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	_ "net/http/pprof"

	"github.com/zhiqiangxu/qrpc"
)

const (
	addr = "0.0.0.0:8001"
	n    = 100000
)

// TestConnection tests connection
func TestNonStream(t *testing.T) {

	go startServer()
	time.Sleep(time.Second * 2)

	conf := qrpc.ConnectionConfig{}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	if err != nil {
		panic(err)
	}

	for _, flag := range []qrpc.FrameFlag{0, qrpc.NBFlag} {
		_, resp, err := conn.Request(HelloCmd, flag, []byte("xu"))
		if err != nil {
			panic(err)
		}
		frame, err := resp.GetFrame()
		if err != nil {
			panic(err)
		}
		fmt.Println("resp is ", string(frame.Payload))
	}

}

func TestCancel(t *testing.T) {

	go startServerForCancel()
	time.Sleep(time.Second * 2)

	conf := qrpc.ConnectionConfig{}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	if err != nil {
		panic(err)
	}

	requestID, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
	if err != nil {
		panic(err)
	}

	fmt.Println("requestID", requestID)
	err = conn.ResetFrame(requestID, 0)
	if err != nil {
		panic(err)
	}
	frame, err := resp.GetFrame()
	if err != nil {
		panic(err)
	}
	fmt.Println("resp is ", string(frame.Payload))

}

func TestPerformance(t *testing.T) {

	srv := &http.Server{Addr: "0.0.0.0:8888"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		runtime.GC()
		io.WriteString(w, "hello world xu\n")
	})
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()

	go startServer()
	conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{}, nil)
	if err != nil {
		panic(err)
	}
	i := 0
	var wg sync.WaitGroup
	startTime := time.Now()
	for {
		_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
		if err != nil {
			panic(err)
		}

		qrpc.GoFunc(&wg, func() {
			frame, _ := resp.GetFrame()
			if !reflect.DeepEqual(frame.Payload, []byte("hello world xu")) {
				panic("fail")
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

func TestAPI(t *testing.T) {

	srv := &http.Server{Addr: "0.0.0.0:8888"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		runtime.GC()
		io.WriteString(w, "hello world xu\n")
	})
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()

	go startServer()
	api := qrpc.NewAPI([]string{addr}, qrpc.ConnectionConfig{}, nil)
	i := 0
	var wg sync.WaitGroup
	startTime := time.Now()
	for {
		qrpc.GoFunc(&wg, func() {
			frame, err := api.Call(context.Background(), HelloCmd, []byte("xu"))
			if err != nil {
				panic(err)
			}
			if !reflect.DeepEqual(frame.Payload, []byte("hello world xu")) {
				panic("fail")
			}
		})
		i++
		if i > n {
			break
		}
	}

	wg.Wait()
	endTime := time.Now()
	fmt.Println(n, "request took", endTime.Sub(startTime))
	select {}
}

func TestPerformanceShort(t *testing.T) {

	srv := &http.Server{Addr: "0.0.0.0:8888"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		runtime.GC()
		io.WriteString(w, "hello world xu\n")
	})
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()

	go startServer()

	time.Sleep(time.Second * 2)
	i := 0
	var wg sync.WaitGroup
	startTime := time.Now()

	for {
		qrpc.GoFunc(&wg, func() {
			conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{}, nil)
			if err != nil {
				panic(err)
			}
			defer conn.Close()
			_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
			if err != nil {
				panic(err)
			}

			frame, err := resp.GetFrame()
			if err != nil {
				panic(err)
			}
			if !reflect.DeepEqual(frame.Payload, []byte("hello world xu")) {
				panic("fail")
			}
		})
		i++
		if i > n {
			break
		}
	}
	wg.Wait()
	endTime := time.Now()
	fmt.Println(n, "request took", endTime.Sub(startTime))

	select {}

}

func TestHTTPPerformance(t *testing.T) {
	srv := &http.Server{Addr: addr}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello world xu")
	})
	go srv.ListenAndServe()
	time.Sleep(time.Second * 2)
	i := 0
	var wg sync.WaitGroup
	startTime := time.Now()

	for {
		resp, err := http.Get("http://" + addr)
		if err != nil {
			panic(err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		if !reflect.DeepEqual(body, []byte("hello world xu")) {
			panic("fail")
		}
		resp.Body.Close()
		if err != nil {
			panic(err)
		}
		if i > n {
			break
		}
		i++
	}
	wg.Wait()
	endTime := time.Now()
	fmt.Println(n, "request took", endTime.Sub(startTime))
}

// func TestGRPCPerformance(t *testing.T) {
// 	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
// 	if err != nil {
// 		panic(err)
// 	}
// 	c := pb.NewGreeterClient(conn)
// 	name := "xu"
// 	startTime := time.Now()
// 	i := 0
// 	for {
// 		_, err := c.SayHello(context.Background(), &pb.HelloRequest{Name: name})
// 		if err != nil {
// 			panic(err)
// 		}
// 		i++

// 		if i > n {
// 			break
// 		}
// 	}
// 	endTime := time.Now()
// 	fmt.Println(n, "request took", endTime.Sub(startTime))
// }

const (
	HelloCmd qrpc.Cmd = iota
	HelloRespCmd
	ClientCmd
	ClientRespCmd
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
		qrpc.ServerBinding{Addr: addr, Handler: handler, ReadFrameChSize: 10000}}
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
		fmt.Println("ListenAndServe", err)
		panic(err)
	}
}

func TestClientHandler(t *testing.T) {
	go startServerForClientHandler()

	conf := qrpc.ConnectionConfig{Handler: qrpc.HandlerFunc(func(w qrpc.FrameWriter, frame *qrpc.RequestFrame) {
		w.StartWrite(frame.RequestID, ClientRespCmd, 0)
		w.WriteBytes([]byte("client resp"))
		w.EndWrite()
	})}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(string(frame.Payload))
	})
	if err != nil {
		panic(err)
	}

	_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu "))
	if err != nil {
		panic(err)
	}

	frame, err := resp.GetFrame()
	if err != nil {
		panic(err)
	}
	fmt.Println("resp is ", string(frame.Payload))
}

func startServerForClientHandler() {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		_, resp, _ := request.ConnectionInfo().SC.Request(ClientCmd, 0, nil)
		frame, _ := resp.GetFrame()
		writer.StartWrite(request.RequestID, HelloRespCmd, 0)
		writer.WriteBytes(request.Payload)
		writer.WriteBytes(frame.Payload)
		writer.EndWrite()
	})
	bindings := []qrpc.ServerBinding{
		qrpc.ServerBinding{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)
	err := server.ListenAndServe()
	if err != nil {
		fmt.Println("ListenAndServe", err)
		panic(err)
	}
}
