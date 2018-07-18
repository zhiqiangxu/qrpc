package test

import (
	"qrpc"
	"testing"
	"time"
)

const (
	addr = "0.0.0.0:8080"
)

// TestConnection tests connection
func TestConnection(t *testing.T) {

	go startServer()
	time.Sleep(time.Second * 2)

	conf := qrpc.ConnectionConfig{}
	cli := qrpc.NewClient(conf)

	cli.GetConn("ab")
}

func startServer() {
	handler := qrpc.NewServeMux()
	bindings := []qrpc.ServerBinding{
		qrpc.ServerBinding{Addr: addr, Handler: handler}}
	server := qrpc.NewServer(bindings)
	server.ListenAndServe()
}
