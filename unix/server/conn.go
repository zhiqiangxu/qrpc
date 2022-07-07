package server

import (
	"fmt"
	"net"
	"time"

	"github.com/zhiqiangxu/qrpc"
)

type unixConn struct {
	conn net.Conn
}

func (c *unixConn) Read(b []byte) (n int, err error) {
	return c.conn.Read(b)
}

func (c *unixConn) Write(b []byte) (n int, err error) {
	return c.conn.Write(b)
}

func (c *unixConn) Close() error {
	return c.conn.Close()
}

func (c *unixConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *unixConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *unixConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *unixConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *unixConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *unixConn) SetKeepAlive(keepalive bool) error {
	return nil
}

func (c *unixConn) SetKeepAlivePeriod(d time.Duration) error {
	return nil
}

func (c *unixConn) SetWriteBuffer(bytes int) error {
	return fmt.Errorf("SetWriteBuffer not implemented for quic")
}

func (c *unixConn) SetReadBuffer(bytes int) error {
	return fmt.Errorf("SetReadBuffer not implemented for quic")
}

func DialFunc(address string, dialConfig qrpc.DialConfig) (con net.Conn, err error) {

	conn, err := net.Dial("unix", address)
	if err != nil {
		return
	}

	con = &unixConn{conn: conn}
	return
}
