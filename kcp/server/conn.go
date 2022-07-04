package server

import (
	"fmt"
	"net"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

type kcpConn kcp.UDPSession

func (c *kcpConn) Read(b []byte) (n int, err error) {
	return (*kcp.UDPSession)(c).Read(b)
}

func (c *kcpConn) Write(b []byte) (n int, err error) {
	return (*kcp.UDPSession)(c).Write(b)
}

func (c *kcpConn) Close() error {
	return (*kcp.UDPSession)(c).Close()
}

func (c *kcpConn) LocalAddr() net.Addr {
	return (*kcp.UDPSession)(c).LocalAddr()
}

func (c *kcpConn) RemoteAddr() net.Addr {
	return (*kcp.UDPSession)(c).RemoteAddr()
}

func (c *kcpConn) SetDeadline(t time.Time) error {
	return (*kcp.UDPSession)(c).SetDeadline(t)
}

func (c *kcpConn) SetReadDeadline(t time.Time) error {
	return (*kcp.UDPSession)(c).SetReadDeadline(t)
}

func (c *kcpConn) SetWriteDeadline(t time.Time) error {
	return (*kcp.UDPSession)(c).SetWriteDeadline(t)
}

func (c *kcpConn) SetKeepAlive(keepalive bool) error {
	return fmt.Errorf("SetKeepAlive not implemented for quic")
}

func (c *kcpConn) SetKeepAlivePeriod(d time.Duration) error {
	return fmt.Errorf("SetKeepAlivePeriod not implemented for quic")
}

func (c *kcpConn) SetWriteBuffer(bytes int) error {
	return (*kcp.UDPSession)(c).SetWriteBuffer(bytes)
}

func (c *kcpConn) SetReadBuffer(bytes int) error {
	return (*kcp.UDPSession)(c).SetReadBuffer(bytes)
}
