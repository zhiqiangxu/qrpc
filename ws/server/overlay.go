package server

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhiqiangxu/qrpc"
)

const (
	backlog = 128
)

// OverlayNetwork impl the overlay network for ws
func OverlayNetwork(l net.Listener) qrpc.Listener {
	return newOverlay(l)
}

type qrpcOverWS struct {
	l          net.Listener
	httpServer *http.Server
	acceptCh   chan *websocket.Conn
	ctx        context.Context
	cancelFunc context.CancelFunc
	deadLine   time.Time
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{"null"},
}

func newOverlay(l net.Listener) (o *qrpcOverWS) {

	mux := &http.ServeMux{}
	mux.HandleFunc("/qrpc", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			qrpc.LogError("upgrader.Upgrade err", err)
			return
		}

		select {
		case o.acceptCh <- c:
		case <-o.ctx.Done():
		}

	})

	httpServer := &http.Server{Handler: mux}
	ctx, cancelFunc := context.WithCancel(context.Background())
	o = &qrpcOverWS{
		l:          l,
		httpServer: httpServer,
		acceptCh:   make(chan *websocket.Conn, backlog),
		ctx:        ctx,
		cancelFunc: cancelFunc,
	}
	return
}

func (o *qrpcOverWS) Accept() (conn net.Conn, err error) {
	switch {
	case o.deadLine.IsZero():
		select {
		case c := <-o.acceptCh:
			conn = NewConn(c)
			return
		case <-o.ctx.Done():
			err = o.ctx.Err()
			return
		}
	default:
		ctx, cancelFunc := context.WithDeadline(context.Background(), o.deadLine)
		defer cancelFunc()
		select {
		case c := <-o.acceptCh:
			conn = NewConn(c)
			return
		case <-o.ctx.Done():
			err = o.ctx.Err()
			return
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
	}
}

func (o *qrpcOverWS) Close() error {
	o.cancelFunc()
	return o.l.Close()
}

func (o *qrpcOverWS) Addr() (addr net.Addr) {
	return o.l.Addr()
}

func (o *qrpcOverWS) SetDeadline(t time.Time) error {
	o.deadLine = t
	return nil
}
