package server

import (
	"net"
	"time"

	"github.com/zhiqiangxu/qrpc"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// conn impl net.Conn for websocket overlay
type conn struct {
	wc     *websocket.Conn
	buffer []byte
}

// NewConn is ctor for conn
func NewConn(wc *websocket.Conn) net.Conn {
	return &conn{wc: wc}
}

func (c *conn) Read(b []byte) (n int, err error) {
	if len(c.buffer) > 0 {
		return c.readBuffer(b)
	}

	var (
		msgType int
		msg     []byte
	)
	for {
		msgType, msg, err = c.wc.ReadMessage()
		if err != nil {
			return
		}
		switch {
		case msgType == websocket.CloseMessage:
			err = c.wc.Close()
			if err != nil {
				return
			}
		case msgType == websocket.PingMessage || msgType == websocket.PongMessage || msgType == websocket.TextMessage:
			qrpc.Logger().Error("got ping/pong/text msg", zap.Int("msgType", msgType), zap.ByteString("msg", msg))
			err = c.wc.Close()
			if err != nil {
				return
			}
		case len(msg) == 0:
			qrpc.Logger().Error("msg length zero")
			continue
		default:
			c.buffer = msg
			return c.readBuffer(b)
		}
	}

}

func (c *conn) readBuffer(b []byte) (n int, err error) {
	n = copy(b, c.buffer)
	c.buffer = c.buffer[n:]
	return
}
func (c *conn) Write(b []byte) (n int, err error) {
	return len(b), c.wc.WriteMessage(websocket.BinaryMessage, b)
}

func (c *conn) Close() (err error) {
	return c.wc.Close()
}

// LocalAddr returns the local network address.
func (c *conn) LocalAddr() (addr net.Addr) {
	return c.wc.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *conn) RemoteAddr() (addr net.Addr) {
	return c.wc.RemoteAddr()
}

func (c *conn) SetDeadline(t time.Time) (err error) {
	err = c.SetReadDeadline(t)
	if err != nil {
		return
	}
	return c.SetWriteDeadline(t)
}

func (c *conn) SetReadDeadline(t time.Time) (err error) {
	return c.wc.SetReadDeadline(t)
}

func (c *conn) SetWriteDeadline(t time.Time) (err error) {
	return c.wc.SetWriteDeadline(t)
}

// getTCPConn returns the underlying tcp conn
func (c *conn) getTCPConn() *net.TCPConn {
	return c.wc.UnderlyingConn().(*net.TCPConn)
}

func (c *conn) SetKeepAlive(keepalive bool) (err error) {
	return c.getTCPConn().SetKeepAlive(keepalive)
}

func (c *conn) SetKeepAlivePeriod(d time.Duration) (err error) {
	return c.getTCPConn().SetKeepAlivePeriod(d)
}

func (c *conn) SetWriteBuffer(bytes int) error {
	return c.getTCPConn().SetWriteBuffer(bytes)
}

func (c *conn) SetReadBuffer(bytes int) error {
	return c.getTCPConn().SetReadBuffer(bytes)
}
