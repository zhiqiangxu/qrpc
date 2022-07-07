package server

import (
	"net"
)

type unixListener struct {
	listener net.Listener
}

func (l unixListener) Accept() (conn net.Conn, err error) {
	con, err := l.listener.Accept()
	if err != nil {
		return
	}

	conn = &unixConn{conn: con}
	return
}

func (l unixListener) Close() error {
	return l.listener.Close()
}

func (l unixListener) Addr() net.Addr {
	return l.listener.Addr()
}
