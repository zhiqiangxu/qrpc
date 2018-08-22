package qrpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	// ErrWriteAfterCloseSelf when try to write after closeself
	ErrWriteAfterCloseSelf = errors.New("write after closeself")
	// ErrRstNonExistingStream when reset non existing stream
	ErrRstNonExistingStream = errors.New("reset non existing stream")
)

// FrameWriter looks like writes a qrpc resp
// but it internally needs be scheduled, thus maintains a simple yet powerful interface
type FrameWriter interface {
	StartWrite(requestID uint64, cmd Cmd, flags FrameFlag)
	WriteBytes(v []byte) // v is copied in WriteBytes
	EndWrite() error     // block until scheduled

	ResetFrame(requestID uint64, reason Cmd) error
}

// A Handler responds to an qrpc request.
type Handler interface {
	ServeQRPC(FrameWriter, *RequestFrame)
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as qrpc handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler that calls f.
type HandlerFunc func(FrameWriter, *RequestFrame)

// ServeQRPC calls f(w, r).
func (f HandlerFunc) ServeQRPC(w FrameWriter, r *RequestFrame) {
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
func (mux *ServeMux) HandleFunc(cmd Cmd, handler func(FrameWriter, *RequestFrame)) {
	mux.Handle(cmd, HandlerFunc(handler))
}

// Handle registers the handler for the given pattern.
// If a handler already exists for pattern, handle panics.
func (mux *ServeMux) Handle(cmd Cmd, handler Handler) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	if handler == nil {
		panic("qrpc: nil handler")
	}
	if _, exist := mux.m[cmd]; exist {
		panic("qrpc: multiple registrations for " + string(cmd))
	}

	if mux.m == nil {
		mux.m = make(map[Cmd]Handler)
	}
	mux.m[cmd] = handler
}

// ServeQRPC dispatches the request to the handler whose
// cmd matches the request.
func (mux *ServeMux) ServeQRPC(w FrameWriter, r *RequestFrame) {
	mux.mu.RLock()
	h, ok := mux.m[r.Cmd]
	if !ok {
		r.Close()
		return
	}
	mux.mu.RUnlock()
	h.ServeQRPC(w, r)
}

// Server defines parameters for running an qrpc server.
type Server struct {
	// one handler for each listening address
	bindings []ServerBinding
	upTime   time.Time

	// manages below
	mu        sync.Mutex
	listeners map[net.Listener]struct{}
	doneChan  chan struct{}

	id2Conn    []sync.Map
	activeConn []sync.Map // for better iterate when write, map[*serveconn]struct{}

	wg sync.WaitGroup // wait group for goroutines

	pushID uint64
}

// NewServer creates a server
func NewServer(bindings []ServerBinding) *Server {
	return &Server{
		bindings:   bindings,
		upTime:     time.Now(),
		listeners:  make(map[net.Listener]struct{}),
		doneChan:   make(chan struct{}),
		id2Conn:    make([]sync.Map, len(bindings)),
		activeConn: make([]sync.Map, len(bindings))}
}

// ListenAndServe starts listening on all bindings
func (srv *Server) ListenAndServe() error {

	for i, binding := range srv.bindings {

		ln, err := net.Listen("tcp", binding.Addr)
		if err != nil {
			srv.Shutdown()
			return err
		}

		idx := i
		GoFunc(&srv.wg, func() {
			srv.serve(tcpKeepAliveListener{ln.(*net.TCPListener)}, idx)
		})

	}

	srv.wg.Wait()
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
func (srv *Server) serve(l tcpKeepAliveListener, idx int) error {

	defer l.Close()
	var tempDelay time.Duration // how long to sleep on accept failure

	srv.trackListener(l, true)
	defer srv.trackListener(l, false)

	serveCtx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	for {
		l.SetDeadline(time.Now().Add(defaultAcceptTimeout))
		rw, e := l.AcceptTCP()
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
				logError("qrpc: Accept error", e, "retrying in", tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c := srv.newConn(serveCtx, rw, idx)

		GoFunc(&srv.wg, func() {
			c.serve()
		})
	}
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
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
func (srv *Server) newConn(ctx context.Context, rwc net.Conn, idx int) *serveconn {
	sc := &serveconn{
		server:       srv,
		rwc:          rwc,
		idx:          idx,
		untrackedCh:  make(chan struct{}),
		cs:           newConnStreams(),
		readFrameCh:  make(chan readFrameResult),
		writeFrameCh: make(chan writeFrameRequest)}

	ctx, cancelCtx := context.WithCancel(ctx)
	ctx = context.WithValue(ctx, ConnectionInfoKey, &ConnectionInfo{SC: sc})

	sc.cancelCtx = cancelCtx
	sc.ctx = ctx

	srv.activeConn[idx].Store(sc, struct{}{})

	return sc
}

// bindID bind the id to sc
// it is concurrent safe
func (srv *Server) bindID(sc *serveconn, id string) {

	idx := sc.idx

check:
	v, loaded := srv.id2Conn[idx].LoadOrStore(id, sc)

	if loaded {
		vsc := v.(*serveconn)
		if vsc == sc {
			return
		}
		ok, ch := srv.untrack(vsc)
		if !ok {
			<-ch
		}
		logDebug(unsafe.Pointer(sc), "trigger closeUntracked", unsafe.Pointer(vsc))
		vsc.closeUntracked()

		goto check
	}
}

func (srv *Server) untrack(sc *serveconn) (bool, <-chan struct{}) {

	locked := atomic.CompareAndSwapUint32(&sc.untrack, 0, 1)
	if !locked {
		return false, sc.untrackedCh
	}
	idx := sc.idx

	id := sc.GetID()
	if id != "" {
		srv.id2Conn[idx].Delete(id)
	}
	srv.activeConn[idx].Delete(sc)

	close(sc.untrackedCh)
	return true, sc.untrackedCh
}

var shutdownPollInterval = 500 * time.Millisecond

// Shutdown gracefully shutdown the server
func (srv *Server) Shutdown() error {

	srv.mu.Lock()
	lnerr := srv.closeListenersLocked()
	if lnerr != nil {
		return lnerr
	}
	srv.mu.Unlock()

	close(srv.doneChan)

	srv.wg.Wait()

	return nil
}

// GetPushID gets the pushId
func (srv *Server) GetPushID() uint64 {
	pushID := atomic.AddUint64(&srv.pushID, 1)
	return pushID
}

// WalkConnByID iterates over  serveconn by ids
func (srv *Server) WalkConnByID(idx int, ids []string, f func(FrameWriter, *ConnectionInfo)) {
	for _, id := range ids {
		v, ok := srv.id2Conn[idx].Load(id)
		if ok {
			sc := v.(*serveconn)
			f(v.(*serveconn).GetWriter(), sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo))
		}
	}
}

// WalkConn walks through each serveconn
func (srv *Server) WalkConn(idx int, f func(FrameWriter, *ConnectionInfo) bool) {
	srv.activeConn[idx].Range(func(k, v interface{}) bool {
		sc := k.(*serveconn)
		return f(sc.GetWriter(), sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo))
	})
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
