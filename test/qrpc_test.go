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
	"sync"
	"testing"
	"time"

	_ "net/http/pprof"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/channel"
	"github.com/zhiqiangxu/util"
	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

const (
	addr = "0.0.0.0:8001"
	n    = 100000
)

// TestStreamInfo tests StreamInfo
func TestStreamInfo(t *testing.T) {

	go startServerForMw()
	time.Sleep(time.Millisecond * 500)

	conf := qrpc.ConnectionConfig{}

	conn, err := qrpc.NewConnection(addr, conf, func(conn *qrpc.Connection, frame *qrpc.Frame) {
		fmt.Println(frame)
	})
	if err != nil {
		panic(err)
	}

	_, resp, err := conn.Request(HelloCmd, 0, []byte(" wxx"))
	if err != nil {
		panic(err)
	}
	frame, err := resp.GetFrame()
	if err != nil {
		panic(err)
	}
	fmt.Println("resp is ", string(frame.Payload))

}


func mW(_ qrpc.FrameWriter, r *qrpc.RequestFrame)  bool {
	streamInfo := r.StreamInfo()
	streamInfo.SetAnything("stream info for mw")
	return true
}

func startServerForMw() {
	handler := qrpc.NewServeMux()
	handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
		// time.Sleep(time.Hour)

		writer.StartWrite(request.RequestID, HelloRespCmd, 0)
		anything := request.StreamInfo().GetAnything()
		writer.WriteBytes(append([]byte("hello " + anything.(string)), request.Payload...))
		err := writer.EndWrite()
		if err != nil {
			fmt.Println("EndWrite", err)
		}
	}, mW)
	bindings := []qrpc.ServerBinding{
		qrpc.ServerBinding{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)
	err := server.ListenAndServe()
	if err != nil {
		fmt.Println("ListenAndServe", err)
		panic(err)
	}
}


// TestConnection tests connection
func TestNonStream(t *testing.T) {

	go startServer()
	time.Sleep(time.Millisecond * 500)

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
	time.Sleep(time.Millisecond * 500)

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

	srv := &http.Server{Addr: "0.0.0.0:9999"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello world xu\n")
	})
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()

	time.Sleep(time.Second)
	go startServer()
	time.Sleep(time.Second)
	conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{WriteFrameChSize: 1000}, nil)
	if err != nil {
		panic(err)
	}
	i := 0
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

func TestAPI(t *testing.T) {

	srv := &http.Server{Addr: "0.0.0.0:8888"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

func TestPerformanceShort(t *testing.T) {

	srv := &http.Server{Addr: "0.0.0.0:8888"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
		if i > n {
			break
		}
	}
	wg.Wait()
	endTime := time.Now()
	fmt.Println(n, "request took", endTime.Sub(startTime))

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
	ClientCmd
	ClientRespCmd
	ChannelCmd
	ChannelRespCmd
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
		qrpc.ServerBinding{Addr: addr, Handler: handler, ReadFrameChSize: 10000, WriteFrameChSize: 1000, WBufSize: 2000000, RBufSize: 2000000}}
	server := qrpc.NewServer(bindings)
	err := server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

type service struct {
}

func (s *service) Hello(ctx context.Context, str string) (r Result) {
	r.Value = "hi " + str
	return
}

type Result struct {
	BaseResp
	Value string
}

type BaseResp struct {
	Err int
	Msg string
}

func (b *BaseResp) OK() bool {
	return b.Err == 0
}

func (b *BaseResp) SetError(err error) {
	if err == nil {
		return
	}
	b.Err = 1
	b.Msg = err.Error()
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

	time.Sleep(time.Second)

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
		_, resp, _ := request.ConnectionInfo().Request(ClientCmd, 0, nil)
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

func TestChannelStyle(t *testing.T) {

	go startServerForChannel()

	time.Sleep(time.Millisecond * 100)

	conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{}, nil)
	if err != nil {
		panic(err)
	}

	transport := channel.NewTransport(conn)
	sender, receiver, err := transport.Pipe()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	msg1 := "hello channel1"
	err = sender.Send(ctx, ChannelCmd, msg1, false)
	if err != nil {
		panic(err)
	}
	msg2 := "hello channel2"
	err = sender.Send(ctx, ChannelCmd, msg2, false)
	if err != nil {
		panic(err)
	}
	var (
		cmd  qrpc.Cmd
		resp string
	)
	err = receiver.Receive(ctx, &cmd, &resp)
	if err != nil {
		panic(err)
	}
	if cmd != ChannelRespCmd || resp != msg1 {
		t.Fatalf("cmd != ChannelRespCmd || resp!=msg1")
	}
	err = receiver.Receive(ctx, &cmd, &resp)
	if err != nil {
		panic(err)
	}
	if cmd != ChannelRespCmd || resp != msg2 {
		t.Fatalf("cmd != ChannelRespCmd || resp!=msg2")
	}
	err = sender.End()
	if err != nil {
		panic(err)
	}
	err = receiver.Receive(ctx, &cmd, &resp)
	if err != channel.ErrStreamFinished {
		t.Fatalf("err != channel.ErrStreamFinished")
	}
}

func startServerForChannel() {
	mux := qrpc.NewServeMux()
	mux.Handle(ChannelCmd, channel.NewQRPCHandler(channel.HandlerFunc(func(s channel.Sender, r channel.Receiver, t channel.Transport) {
		var (
			cmd qrpc.Cmd
			req string
		)
		ctx := context.TODO()

		for {
			err := r.Receive(ctx, &cmd, &req)
			if err != nil {
				if err != channel.ErrStreamFinished {
					panic(fmt.Sprintf("Receive:%v", err))
				}
				err = s.End()
				break
			}
			s.Send(ctx, ChannelRespCmd, req, false)
		}

	})))
	bindings := []qrpc.ServerBinding{
		qrpc.ServerBinding{Addr: addr, Handler: mux}}
	server := qrpc.NewServer(bindings)
	err := server.ListenAndServe()
	if err != nil {
		fmt.Println("ListenAndServe", err)
		panic(err)
	}
}
