package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	_ "net/http/pprof"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/channel"
	"github.com/zhiqiangxu/util"
	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"gotest.tools/v3/assert"
)

const (
	addr = "localhost:8001"
	n    = 100000
)

func TestTCPLatency(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServer(ctx)
	})

	time.Sleep(time.Millisecond * 500)

	conf := qrpc.ConnectionConfig{}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	assert.Assert(t, err == nil)

	payload := bytes.Repeat([]byte("xu"), 200)
	for i := 0; i < 7; i++ {
		start := time.Now()
		_, resp, err := conn.Request(HelloCmd, 0, payload)

		assert.Assert(t, err == nil)

		_, err = resp.GetFrame()

		fmt.Println("single request took", time.Since(start), "startts", start.Second(), start.Nanosecond())

		assert.Assert(t, err == nil)

		fmt.Println("-----------")
	}

	cancelFunc()
	wg.Wait()
}

func TestNonStreamAndPushFlg(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServer(ctx)
	})

	time.Sleep(time.Millisecond * 500)

	conf := qrpc.ConnectionConfig{}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	assert.Assert(t, err == nil)

	for _, flag := range []qrpc.FrameFlag{0, qrpc.NBFlag} {
		start := time.Now()
		_, resp, err := conn.Request(HelloCmd, flag, []byte("xu"))

		assert.Assert(t, err == nil)

		frame, err := resp.GetFrame()

		fmt.Println("single request took", time.Since(start), "startts", start.Second(), start.Nanosecond())

		assert.Assert(t, err == nil)

		_, resp, err = conn.Request(PushCmd, flag|qrpc.PushFlag, []byte("xu"))
		assert.Assert(t, err == nil && resp == nil)

		fmt.Println("resp is ", string(frame.Payload))
	}

	cancelFunc()
	wg.Wait()
}

func TestCancel(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServerForCancel(ctx)
	})

	time.Sleep(time.Millisecond * 500)

	conf := qrpc.ConnectionConfig{}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	assert.Assert(t, err == nil)

	requestID, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
	assert.Assert(t, err == nil)

	fmt.Println("requestID", requestID)
	err = conn.ResetFrame(requestID, 0)
	assert.Assert(t, err == nil)

	frame, err := resp.GetFrame()
	assert.Assert(t, err == nil)

	fmt.Println("resp is ", string(frame.Payload))
	cancelFunc()
	wg.Wait()
}

func TestPerformance(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServer(ctx)
	})
	time.Sleep(time.Second)
	conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{WriteFrameChSize: 1000}, nil)
	if err != nil {
		panic(err)
	}
	i := 0
	payload := bytes.Repeat([]byte("xu"), 200)
	expected := append([]byte("hello world "), payload...)
	{
		var wg sync.WaitGroup
		startTime := time.Now()
		for {

			util.GoFunc(&wg, func() {
				_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, payload)
				if err != nil {
					panic(err)
				}
				frame, err := resp.GetFrame()
				if err != nil || !bytes.Equal(frame.Payload, expected) {
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

func TestAPI(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServer(ctx)
	})
	time.Sleep(time.Second)
	api := qrpc.NewAPI([]string{addr}, qrpc.ConnectionConfig{}, nil)
	i := 0
	{
		var wg sync.WaitGroup
		startTime := time.Now()
		for {
			util.GoFunc(&wg, func() {
				frame, err := api.Call(context.Background(), HelloCmd, []byte("xu"))
				if err != nil {
					panic(err)
				}
				if !bytes.Equal(frame.Payload, []byte("hello world xu")) {
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
	}

	cancelFunc()
	wg.Wait()
}

func TestPerformanceShort(t *testing.T) {

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServer(ctx)
	})

	{
		time.Sleep(time.Second * 1)
		i := 0
		var wg sync.WaitGroup
		startTime := time.Now()

		sn := n / 100
		if runtime.GOOS == "darwin" {
			sn /= 10
		}
		for {
			util.GoFunc(&wg, func() {
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
				if !bytes.Equal(frame.Payload, []byte("hello world xu")) {
					panic("fail")
				}
			})
			i++
			if i > sn {
				break
			}
		}
		wg.Wait()
		endTime := time.Now()
		fmt.Println(sn, "request took", endTime.Sub(startTime))
	}

	cancelFunc()
	wg.Wait()
}

func TestHTTPPerformance(t *testing.T) {
	srv := &http.Server{Addr: addr}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello world xu")
	})

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		util.RunWithCancel(ctx, func() {
			srv.ListenAndServe()
		}, func() {
			srv.Shutdown(context.Background())
		})

	})
	time.Sleep(time.Second)

	{
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
			if !bytes.Equal(body, []byte("hello world xu")) {
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

	cancelFunc()
	wg.Wait()

}

const grpcAddr = "localhost:50051"

func TestGRPCPerformance(t *testing.T) {
	go startGRPCServer()
	time.Sleep(time.Second)

	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		panic(err)
	}
	c := pb.NewGreeterClient(conn)
	name := "xu"
	startTime := time.Now()
	i := 0
	var wg sync.WaitGroup
	for {
		util.GoFunc(&wg, func() {
			_, err := c.SayHello(context.Background(), &pb.HelloRequest{Name: name})
			if err != nil {
				panic(err)
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
}

// grpcserver is used to implement helloworld.GreeterServer.
type grpcserver struct {
	// pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *grpcserver) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	// log.Printf("Received: %v", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func startGRPCServer() {
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &grpcserver{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

const (
	HelloCmd qrpc.Cmd = iota
	HelloRespCmd
	PushCmd
	ClientCmd
	ClientRespCmd
	ChannelCmd
	ChannelRespCmd
)

func startServer(ctx context.Context) {
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
		{Addr: addr, Handler: handler, SubFunc: subFunc, ReadFrameChSize: 10000, WriteFrameChSize: 1000, WBufSize: 2000000, RBufSize: 2000000}}

	server := qrpc.NewServer(bindings)

	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})

}

func startServerForCancel(ctx context.Context) {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		// time.Sleep(time.Hour)
		<-request.Context().Done()
		writer.StartWrite(request.RequestID, HelloRespCmd, 0)

		writer.WriteBytes(append([]byte("hello canceled "), request.Payload...))
		err := writer.EndWrite()
		if err != nil {
			fmt.Println("EndWrite", err)
		}

	})
	bindings := []qrpc.ServerBinding{
		{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)

	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})

}

func TestClientHandler(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServerForClientHandler(ctx)
	})

	time.Sleep(time.Second)

	conf := qrpc.ConnectionConfig{Handler: qrpc.HandlerFunc(func(w qrpc.FrameWriter, frame *qrpc.RequestFrame) {
		w.StartWrite(frame.RequestID, ClientRespCmd, 0)
		w.WriteBytes([]byte("client resp"))
		w.EndWrite()
	})}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(string(frame.Payload))
	})
	assert.Assert(t, err == nil)

	_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu "))
	assert.Assert(t, err == nil)

	frame, err := resp.GetFrame()
	assert.Assert(t, err == nil)
	fmt.Println("resp is ", string(frame.Payload))

	cancelFunc()
	wg.Wait()
}

func startServerForClientHandler(ctx context.Context) {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		_, resp, _ := request.ConnectionInfo().Request(ClientCmd, 0, nil)
		frame, _ := resp.GetFrame()
		writer.StartWrite(request.RequestID, HelloRespCmd, 0)
		writer.WriteBytes(request.Payload)
		writer.WriteBytes(frame.Payload)
		writer.EndWrite()
	})
	bindings := []qrpc.ServerBinding{
		{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)

	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})

}

func TestChannelStyle(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startServerForChannel(ctx)
	})

	time.Sleep(time.Millisecond * 100)

	conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{}, nil)
	assert.Assert(t, err == nil)

	transport := channel.NewTransport(conn)
	sender, receiver, err := transport.Pipe()
	assert.Assert(t, err == nil)

	msg1 := "hello channel1"
	err = sender.Send(ctx, ChannelCmd, msg1, false)
	assert.Assert(t, err == nil)

	msg2 := "hello channel2"
	err = sender.Send(ctx, ChannelCmd, msg2, false)
	assert.Assert(t, err == nil)

	var (
		cmd  qrpc.Cmd
		resp string
	)
	err = receiver.Receive(ctx, &cmd, &resp)
	assert.Assert(t, err == nil)

	if cmd != ChannelRespCmd || resp != msg1 {
		t.Fatalf("cmd != ChannelRespCmd || resp!=msg1")
	}
	err = receiver.Receive(ctx, &cmd, &resp)
	assert.Assert(t, err == nil)

	if cmd != ChannelRespCmd || resp != msg2 {
		t.Fatalf("cmd != ChannelRespCmd || resp!=msg2")
	}
	err = sender.End()
	assert.Assert(t, err == nil)

	err = receiver.Receive(ctx, &cmd, &resp)
	if err != channel.ErrStreamFinished {
		t.Fatalf("err != channel.ErrStreamFinished")
	}

	cancelFunc()
	wg.Wait()
}

func startServerForChannel(ctx context.Context) {
	mux := qrpc.NewServeMux()
	mux.Handle(ChannelCmd, channel.NewQRPCHandler(channel.HandlerFunc(func(s channel.Sender, r channel.Receiver, t channel.Transport) {
		var (
			cmd qrpc.Cmd
			req string
		)
		ctx := context.TODO()

		var err error
		for {
			err = r.Receive(ctx, &cmd, &req)
			if err != nil {
				if err != channel.ErrStreamFinished {
					panic(fmt.Sprintf("Receive:%v", err))
				}
				err = s.End()
				break
			}
			s.Send(ctx, ChannelRespCmd, req, false)
		}
		fmt.Println("err", err)
	})))
	bindings := []qrpc.ServerBinding{
		{Addr: addr, Handler: mux}}
	server := qrpc.NewServer(bindings)

	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})

}
