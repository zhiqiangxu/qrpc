package ws

import (
	"github.com/zhiqiangxu/qrpc"
)

// NewServer is a wrapper for qrpc.NewServer
func NewServer(bindings []qrpc.ServerBinding) *qrpc.Server {
	for i := range bindings {
		bindings[i].OverlayNetwork = OverlayNetwork
	}
	return qrpc.NewServer(bindings)
}
