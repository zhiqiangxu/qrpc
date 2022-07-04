package server

import (
	"net"

	"github.com/xtaci/kcp-go/v5"
)

type kcpListener kcp.Listener

func (l *kcpListener) Accept() (conn net.Conn, err error) {
	con, err := (*kcp.Listener)(l).AcceptKCP()
	if err != nil {
		return
	}

	conn = (*kcpConn)(con)
	return
}

func (l *kcpListener) Close() error {
	return (*kcp.Listener)(l).Close()
}

func (l *kcpListener) Addr() net.Addr {
	return (*kcp.Listener)(l).Addr()
}
