package client

import (
	"net"
	"net/http"
	"time"

	"github.com/zhiqiangxu/qrpc"

	"github.com/zhiqiangxu/qrpc/ws/server"

	"github.com/gorilla/websocket"
)

// DialConn is ctor for conn
func DialConn(address string, timeout time.Duration) (nc net.Conn, err error) {
	var (
		wc   *websocket.Conn
		resp *http.Response
	)

	dialer := &websocket.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			return net.DialTimeout(network, addr, timeout)
		},
	}
	wc, resp, err = dialer.Dial("ws://"+address+"/qrpc", http.Header{})
	if err != nil {
		qrpc.LogError("dialer.Dial err", err, "resp", resp)
		return
	}

	nc = server.NewConn(wc)
	return
}
