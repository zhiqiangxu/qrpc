package client

import (
	"net"
	"net/http"

	"github.com/zhiqiangxu/qrpc"

	"github.com/zhiqiangxu/qrpc/ws/server"

	"github.com/gorilla/websocket"
)

// DialConn is ctor for conn
func DialConn(address string, dialConfig qrpc.DialConfig) (nc net.Conn, err error) {
	var (
		wc   *websocket.Conn
		resp *http.Response
	)

	dialer := &websocket.Dialer{
		NetDial: func(network, addr string) (conn net.Conn, err error) {
			conn, err = net.DialTimeout(network, addr, dialConfig.DialTimeout)
			if err != nil {
				qrpc.LogError("DialConn net.DialTimeout err", err, "addr", addr)
				return
			}
			if dialConfig.RBufSize <= 0 && dialConfig.WBufSize <= 0 {
				return
			}
			tc := conn.(*net.TCPConn)
			if dialConfig.RBufSize > 0 {
				sockOptErr := tc.SetReadBuffer(dialConfig.RBufSize)
				if sockOptErr != nil {
					qrpc.LogError("SetReadBuffer err", err, "RBufSize", dialConfig.RBufSize)
				}
			}
			if dialConfig.WBufSize > 0 {
				sockOptErr := tc.SetWriteBuffer(dialConfig.WBufSize)
				if sockOptErr != nil {
					qrpc.LogError("SetWriteBuffer err", err, "WBufSize", dialConfig.WBufSize)
				}
			}
			return
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
