package server

import (
	"context"
	"net"

	"github.com/lucas-clemente/quic-go"
)

type quicListener struct {
	listener quic.Listener
}

func (l quicListener) Accept() (conn net.Conn, err error) {
	con, err := l.listener.Accept(context.Background())
	if err != nil {
		return
	}

	stream, err := con.AcceptStream(context.Background())
	if err != nil {
		return
	}

	conn = &quicConn{conn: con, stream: stream}
	return
}

func (l quicListener) Close() error {
	return l.listener.Close()
}

func (l quicListener) Addr() net.Addr {
	return l.listener.Addr()
}
