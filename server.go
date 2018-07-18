package qrpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// FrameWriter looks like writes a qrpc resp
// but it internally needs be scheduled, thus maintains a simple yet powerful interface
type FrameWriter interface {
	StartWrite(requestID uint64, cmd Cmd, flags PacketFlag)
	WriteBytes(v []byte) // v is copied in WriteBytes
	EndWrite() error     // block until scheduled
}

// A Handler responds to an qrpc request.
type Handler interface {
	ServeQRPC(FrameWriter, *Frame)
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as qrpc handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler that calls f.
type HandlerFunc func(FrameWriter, *Frame)

// ServeQRPC calls f(w, r).
func (f HandlerFunc) ServeQRPC(w FrameWriter, r *Frame) {
	f(w, r)
}

// ServeMux is qrpc request multiplexer.
type ServeMux struct {
	mu sync.RWMutex
	m  map[Cmd]Handler
}

// NewServeMux allocates and returns a new ServeMux.
func NewServeMux() *ServeMux { return new(ServeMux) }

// HandleFunc registers the handler function for the given pattern.
func (mux *ServeMux) HandleFunc(cmd Cmd, handler func(FrameWriter, *Frame)) {
	mux.handle(cmd, HandlerFunc(handler))
}

// handle registers the handler for the given pattern.
// If a handler already exists for pattern, handle panics.
func (mux *ServeMux) handle(cmd Cmd, handler Handler) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	if handler == nil {
		panic("http: nil handler")
	}
	if _, exist := mux.m[cmd]; exist {
		panic("http: multiple registrations for " + string(cmd))
	}

	if mux.m == nil {
		mux.m = make(map[Cmd]Handler)
	}
	mux.m[cmd] = handler
}

// ServeQRPC dispatches the request to the handler whose
// cmd matches the request.
func (mux *ServeMux) ServeQRPC(w FrameWriter, r *Frame) {
	mux.mu.RLock()
	h, ok := mux.m[r.Cmd]
	if !ok {
		return
	}
	mux.mu.RUnlock()
	h.ServeQRPC(w, r)
}

// Server defines parameters for running an qrpc server.
type Server struct {
	// one handler for each listening address
	bindings []ServerBinding

	// manages below two
	mu        sync.Mutex
	listeners map[net.Listener]struct{}
	doneChan  chan struct{}

	activeConn []sync.Map // for better iterate when write, map[conn]*ConnectionInfo

	pushID uint64
}

// NewServer creates a server
func NewServer(bindings []ServerBinding) *Server {
	return &Server{
		bindings:   bindings,
		listeners:  make(map[net.Listener]struct{}),
		doneChan:   make(chan struct{}),
		activeConn: make([]sync.Map, len(bindings))}
}

// ListenAndServe starts listening on all bindings
func (srv *Server) ListenAndServe() error {

	for idx, binding := range srv.bindings {
		ln, err := net.Listen("tcp", binding.Addr)
		if err != nil {
			srv.Shutdown(nil)
			return err
		}

		go srv.serve(ln, idx)

	}

	return nil
}

// ErrServerClosed is returned by the Server's Serve, ListenAndServe,
// methods after a call to Shutdown or Close.
var ErrServerClosed = errors.New("qrpc: Server closed")

var defaultAcceptTimeout = 5 * time.Second

// serve accepts incoming connections on the Listener l, creating a
// new service goroutine for each. The service goroutines read requests and
// then call srv.Handler to reply to them.
//
// serve always returns a non-nil error. After Shutdown or Close, the
// returned error is ErrServerClosed.
func (srv *Server) serve(l net.Listener, idx int) error {

	defer l.Close()
	var tempDelay time.Duration // how long to sleep on accept failure

	srv.trackListener(l, true)
	defer srv.trackListener(l, false)

	serveCtx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	for {
		l.(*net.TCPListener).SetDeadline(time.Now().Add(defaultAcceptTimeout))
		rw, e := l.Accept()
		if e != nil {
			select {
			case <-srv.doneChan:
				return ErrServerClosed
			default:
			}
			if opError, ok := e.(*net.OpError); ok && opError.Timeout() {
				// don't log the scheduled timeout
				continue
			}
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				srv.logf("http: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c := srv.newConn(rw)

		go c.serve(serveCtx, idx)
	}
}

func (srv *Server) trackListener(ln net.Listener, add bool) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if add {
		srv.listeners[ln] = struct{}{}
	} else {
		delete(srv.listeners, ln)
	}
}

// Create new connection from rwc.
func (srv *Server) newConn(rwc net.Conn) *conn {
	c := &conn{
		server:       srv,
		rwc:          rwc,
		readFrameCh:  make(chan readFrameResult),
		writeFrameCh: make(chan writeFrameRequest)}
	return c
}

func (srv *Server) logf(format string, args ...interface{}) {

}

var shutdownPollInterval = 500 * time.Millisecond

// Shutdown gracefully shutdown the server
func (srv *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	srv.mu.Lock()
	lnerr := srv.closeListenersLocked()
	if lnerr != nil {
		return lnerr
	}
	srv.mu.Unlock()

	close(srv.doneChan)

	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
	for {
		if srv.waitConnDone() {
			return lnerr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// PushFrame pushes a frame to specified connection
// it is thread safe
func (srv *Server) PushFrame(conn *conn, cmd Cmd, flags PacketFlag, payload []byte) error {

	pushID := atomic.AddUint64(&srv.pushID, 1)
	flags &= PushFlag
	w := conn.GetWriter()
	w.StartWrite(pushID, cmd, flags)
	w.WriteBytes(payload)
	return w.EndWrite()

}

func (srv *Server) waitConnDone() bool {
	done := true
	for idx := 0; done && idx < len(srv.bindings); idx++ {
		srv.activeConn[idx].Range(func(key, value interface{}) bool {
			done = false
			return false
		})
	}

	return done
}

func (srv *Server) closeListenersLocked() error {
	var err error
	for ln := range srv.listeners {
		if cerr := ln.Close(); cerr != nil && err == nil {
			err = cerr
		}
		delete(srv.listeners, ln)
	}
	return err
}
