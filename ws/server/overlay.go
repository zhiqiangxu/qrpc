package server

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/zhiqiangxu/qrpc"
	"go.uber.org/zap"
)

const (
	backlog = 128
)

// OverlayNetwork impl the overlay network for ws
func OverlayNetwork(l net.Listener, tlsConfig *tls.Config) qrpc.Listener {
	return newOverlay(l, tlsConfig)
}

type qrpcOverWS struct {
	l          net.Listener
	httpServer *http.Server
	acceptCh   chan *websocket.Conn
	tlsConfig  *tls.Config
	ctx        context.Context
	cancelFunc context.CancelFunc
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{"null"},
}

func newOverlay(l net.Listener, tlsConfig *tls.Config) (o *qrpcOverWS) {

	if tlsConfig != nil {
		l = tls.NewListener(l, tlsConfig)
	}

	mux := &http.ServeMux{}
	mux.HandleFunc("/qrpc", func(w http.ResponseWriter, r *http.Request) {

		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			qrpc.Logger().Error("upgrader.Upgrade", zap.Error(err))
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
		tlsConfig:  tlsConfig,
		ctx:        ctx,
		cancelFunc: cancelFunc,
	}

	go func() {
		err := httpServer.Serve(l)
		qrpc.Logger().Error("httpServer.Serve", zap.Error(err))
	}()

	return
}

func (o *qrpcOverWS) Accept() (conn net.Conn, err error) {
	select {
	case c := <-o.acceptCh:
		conn = NewConn(c)
		return
	case <-o.ctx.Done():
		err = o.ctx.Err()
		return
	}
}

func (o *qrpcOverWS) Close() error {
	o.cancelFunc()
	return o.l.Close()
}

func (o *qrpcOverWS) Addr() (addr net.Addr) {
	return o.l.Addr()
}
