package server

import (
	"net"

	"github.com/xtaci/kcp-go/v5"
	"github.com/zhiqiangxu/qrpc"
	"go.uber.org/zap"
)

// New is a wrapper for qrpc.NewServer
func New(bindings []qrpc.ServerBinding) *qrpc.Server {
	for i, binding := range bindings {
		wBufSize := binding.WBufSize
		rBufSize := binding.RBufSize
		bindings[i].WBufSize = 0
		bindings[i].RBufSize = 0
		bindings[i].ListenFunc = func(network, address string) (l net.Listener, err error) {
			listener, err := kcp.ListenWithOptions(address, nil, 7, 3)
			if err != nil {
				return
			}

			if wBufSize > 0 {
				err = listener.SetWriteBuffer(wBufSize)
				if err != nil {
					qrpc.Logger().Error("kcp SetWriteBuffer", zap.Int("WBufSize", wBufSize), zap.Error(err))
				}
			}
			if rBufSize > 0 {
				err = listener.SetReadBuffer(rBufSize)
				if err != nil {
					qrpc.Logger().Error("kcp SetReadBuffer", zap.Int("RBufSize", rBufSize), zap.Error(err))
				}
			}
			err = nil

			l = (*kcpListener)(listener)
			return
		}
	}
	return qrpc.NewServer(bindings)
}
