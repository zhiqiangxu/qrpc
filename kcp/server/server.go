package server

import (
	"net"

	"github.com/xtaci/kcp-go/v5"
	"github.com/zhiqiangxu/qrpc"
)

// New is a wrapper for qrpc.NewServer
func New(bindings []qrpc.ServerBinding) *qrpc.Server {
	for i := range bindings {
		bindings[i].ListenFunc = func(network, address string) (l net.Listener, err error) {
			listener, err := kcp.ListenWithOptions(address, nil, 7, 3)
			if err != nil {
				return
			}

			l = (*kcpListener)(listener)
			return
		}
	}
	return qrpc.NewServer(bindings)
}
