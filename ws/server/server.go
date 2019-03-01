package server

import (
	"github.com/zhiqiangxu/qrpc"
)

// New is a wrapper for qrpc.NewServer
func New(bindings []qrpc.ServerBinding) *qrpc.Server {
	for i := range bindings {
		bindings[i].OverlayNetwork = OverlayNetwork
	}
	return qrpc.NewServer(bindings)
}
