// +build go1.11

package qrpc

import (
	"net"

	reuse "github.com/zhiqiangxu/go-reuseport"
)

// NewReusedConnection is like NewConnection except the underlying socket can be reused
func NewReusedConnection(addr string, conf ConnectionConfig, f SubFunc) (*Connection, error) {
	rwc, err := reuse.DialWithTimeout("tcp", "", addr, conf.DialTimeout)
	if err != nil {
		logError("NewConnection Dial", err)
		return nil, err
	}

	return newConnection(rwc, []string{addr}, conf, f, false), nil
}

// GetReusedCon returns the underlying reuse-able socket
func (conn *Connection) GetReusedCon() net.Conn {
	return conn.rwc
}
