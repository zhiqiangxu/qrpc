package server

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/zhiqiangxu/qrpc"
)

type quicConn struct {
	conn   quic.Connection
	stream quic.Stream
}

func (c *quicConn) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

func (c *quicConn) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

func (c *quicConn) Close() error {
	return c.stream.Close()
}

func (c *quicConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *quicConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *quicConn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}

func (c *quicConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *quicConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

func (c *quicConn) SetKeepAlive(keepalive bool) error {
	return fmt.Errorf("SetKeepAlive not implemented for quic")
}

func (c *quicConn) SetKeepAlivePeriod(d time.Duration) error {
	return fmt.Errorf("SetKeepAlivePeriod not implemented for quic")
}

func (c *quicConn) SetWriteBuffer(bytes int) error {
	return fmt.Errorf("SetWriteBuffer not implemented for quic")
}

func (c *quicConn) SetReadBuffer(bytes int) error {
	return fmt.Errorf("SetReadBuffer not implemented for quic")
}

func DialFunc(address string, dialConfig qrpc.DialConfig) (con net.Conn, err error) {

	var conn quic.Connection

	if dialConfig.DialTimeout == 0 {
		conn, err = quic.DialAddr(address, dialConfig.TLSConf, nil)
	} else {
		ctx, cancelFunc := context.WithTimeout(context.Background(), dialConfig.DialTimeout)
		conn, err = quic.DialAddrContext(ctx, address, dialConfig.TLSConf, nil)
		cancelFunc()
	}

	if err != nil {
		return
	}

	if dialConfig.RBufSize != 0 {
		err = fmt.Errorf("RBufSize not implemented for quic")
		return
	}

	if dialConfig.WBufSize != 0 {
		err = fmt.Errorf("WBufSize not implemented for quic")
		return
	}

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return
	}

	con = &quicConn{conn: conn, stream: stream}
	return
}
