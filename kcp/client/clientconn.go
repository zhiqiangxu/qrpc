package client

import (
	"fmt"
	"net"

	"github.com/xtaci/kcp-go/v5"
	"github.com/zhiqiangxu/qrpc"
)

var emptyDialConfig = qrpc.DialConfig{}

// NewConnection is a wrapper for qrpc.NewConnection
func NewConnection(addr string, conf qrpc.ConnectionConfig, f qrpc.SubFunc) (*qrpc.Connection, error) {
	conf.DialFunc = func(address string, dialConfig qrpc.DialConfig) (net.Conn, error) {
		if dialConfig != emptyDialConfig {
			return nil, fmt.Errorf("DialConfig not supported for kcp")
		}
		return kcp.Dial(address)
	}
	return qrpc.NewConnection(addr, conf, f)
}
