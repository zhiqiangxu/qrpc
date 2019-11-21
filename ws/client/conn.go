package client

import (
	"net"
	"net/http"

	"github.com/zhiqiangxu/qrpc"

	"github.com/zhiqiangxu/qrpc/ws/server"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
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
				qrpc.Logger().Error("DialConn net.DialTimeout", zap.String("addr", addr), zap.Error(err))
				return
			}
			if dialConfig.RBufSize <= 0 && dialConfig.WBufSize <= 0 {
				return
			}
			tc := conn.(*net.TCPConn)
			if dialConfig.RBufSize > 0 {
				sockOptErr := tc.SetReadBuffer(dialConfig.RBufSize)
				if sockOptErr != nil {
					qrpc.Logger().Error("SetReadBuffer", zap.Int("RBufSize", dialConfig.RBufSize), zap.Error(sockOptErr))
				}
			}
			if dialConfig.WBufSize > 0 {
				sockOptErr := tc.SetWriteBuffer(dialConfig.WBufSize)
				if sockOptErr != nil {
					qrpc.Logger().Error("SetWriteBuffer", zap.Int("WBufSize", dialConfig.WBufSize), zap.Error(sockOptErr))
				}
			}
			return
		},
	}
	wc, resp, err = dialer.Dial("ws://"+address+"/qrpc", http.Header{})
	if err != nil {
		qrpc.Logger().Error("dialer.Dial", zap.Any("resp", resp), zap.Error(err))
		return
	}

	nc = server.NewConn(wc)
	return
}
