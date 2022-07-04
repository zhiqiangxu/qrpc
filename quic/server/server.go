package server

import (
	"fmt"
	"net"

	"github.com/lucas-clemente/quic-go"
	"github.com/zhiqiangxu/qrpc"
)

// New is a wrapper for qrpc.NewServer
func New(bindings []qrpc.ServerBinding) *qrpc.Server {
	for i := range bindings {
		tlsConfig := bindings[i].TLSConf
		if tlsConfig == nil {
			panic(fmt.Sprintf("TLSConf is mandated for quic, index:%d", i))
		}
		bindings[i].TLSConf = nil
		bindings[i].ListenFunc = func(network, address string) (l net.Listener, err error) {
			listener, err := quic.ListenAddr(bindings[i].Addr, tlsConfig, nil)
			if err != nil {
				return
			}

			return quicListener{listener: listener}, nil
		}
	}
	return qrpc.NewServer(bindings)
}
