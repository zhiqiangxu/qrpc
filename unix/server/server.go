package server

import (
	"net"

	"github.com/zhiqiangxu/qrpc"
)

// New is a wrapper for qrpc.NewServer
func New(bindings []qrpc.ServerBinding) *qrpc.Server {
	for i := range bindings {
		bindings[i].ListenFunc = func(network, address string) (l net.Listener, err error) {
			listener, err := net.Listen("unix", address)
			if err != nil {
				return
			}

			return unixListener{listener: listener}, nil
		}
	}
	return qrpc.NewServer(bindings)
}
