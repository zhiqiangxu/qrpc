package test

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"sync"
	"testing"
	"time"

	"crypto/x509"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/ws/client"
	wsserver "github.com/zhiqiangxu/qrpc/ws/server"
	"github.com/zhiqiangxu/util"
)

func TestTLS(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startTLSServer(ctx)
	})
	time.Sleep(time.Second)
	// 唯一的不同，是传入TLSConf
	conn, err := qrpc.NewConnection(addr, qrpc.ConnectionConfig{TLSConf: clientTLSConfig()}, nil)
	if err != nil {
		panic(err)
	}

	_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
	if err != nil {
		panic(err)
	}
	frame, err := resp.GetFrame()
	if err != nil || !bytes.Equal(frame.Payload, []byte("hello world xu")) {
		panic(fmt.Sprintf("fail payload:%s len:%v cmd:%v flags:%v err:%v", string(frame.Payload), len(frame.Payload), frame.Cmd, frame.Flags, err))
	}

	cancelFunc()
	wg.Wait()
}

func TestWSTLS(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	util.GoFunc(&wg, func() {
		startWSTLSServer(ctx)
		log.Println("startWSTLSServer done")
	})
	time.Sleep(time.Second)
	// 唯一的不同，是传入TLSConf
	conn, err := client.NewConnection(addr, qrpc.ConnectionConfig{TLSConf: clientTLSConfig()}, nil)
	if err != nil {
		panic(err)
	}

	_, resp, err := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
	if err != nil {
		panic(err)
	}
	frame, err := resp.GetFrame()
	if err != nil || !bytes.Equal(frame.Payload, []byte("hello world xu")) {
		panic(fmt.Sprintf("fail payload:%s len:%v cmd:%v flags:%v err:%v", string(frame.Payload), len(frame.Payload), frame.Cmd, frame.Flags, err))
	}

	cancelFunc()
	wg.Wait()
}

func serverTLSConfig() *tls.Config {
	cert, err := tls.LoadX509KeyPair("data/server.pem", "data/server.key")
	if err != nil {
		log.Fatal(err)
	}

	certBytes, err := ioutil.ReadFile("data/client.pem")
	if err != nil {
		log.Fatal(err)
	}
	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(certBytes)
	if !ok {
		log.Fatal("failed to parse root certificate")
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCertPool,
	}

	return config
}

func clientTLSConfig() *tls.Config {
	cert, err := tls.LoadX509KeyPair("data/client.pem", "data/client.key")
	if err != nil {
		log.Fatal(err)
	}
	certBytes, err := ioutil.ReadFile("data/client.pem")
	if err != nil {
		log.Fatal("Unable to read cert.pem")
	}

	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(certBytes)
	if !ok {
		log.Fatal("failed to parse root certificate")
	}

	conf := &tls.Config{
		RootCAs:            clientCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}

	return conf
}

func startTLSServer(ctx context.Context) {
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
	// 唯一的不同，是传入TLSConf
	bindings := []qrpc.ServerBinding{
		{Addr: addr, Handler: handler, TLSConf: serverTLSConfig()}}
	server := qrpc.NewServer(bindings)
	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		server.Shutdown()
	})
}

func startWSTLSServer(ctx context.Context) {
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
	// 唯一的不同，是传入TLSConf
	bindings := []qrpc.ServerBinding{
		{Addr: addr, Handler: handler, TLSConf: serverTLSConfig()}}
	server := wsserver.New(bindings)
	util.RunWithCancel(ctx, func() {
		server.ListenAndServe()
	}, func() {
		log.Println("WSTLSServer shutdown", server.Shutdown())
	})
}
