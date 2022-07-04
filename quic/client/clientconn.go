package client

import (
	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/quic/server"
)

// NewConnection is a wrapper for qrpc.NewConnection
func NewConnection(addr string, conf qrpc.ConnectionConfig, f qrpc.SubFunc) (*qrpc.Connection, error) {
	conf.DialFunc = server.DialFunc
	return qrpc.NewConnection(addr, conf, f)
}
