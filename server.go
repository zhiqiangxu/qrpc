package qrpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/oklog/run"
	"go.uber.org/ratelimit"
	"go.uber.org/zap"
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
	EndWriteCompressed() error

	ResetFrame(requestID uint64, reason Cmd) error
}

// StreamWriter is returned by StreamRequest
type StreamWriter interface {
	RequestID() uint64
	StartWrite(cmd Cmd)
	WriteBytes(v []byte)     // v is copied in WriteBytes
	EndWrite(end bool) error // block until scheduled
	EndWriteCompressed() error
	ResetFrame(reason Cmd) error
}

// A Handler responds to an qrpc request.
type Handler interface {
	// FrameWriter will be recycled when ServeQRPC finishes, so don't cache it
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

// MiddlewareFunc will return false to abort
type MiddlewareFunc func(FrameWriter, *RequestFrame) bool

// ServeMux is qrpc request multiplexer.
type ServeMux struct {
	mu sync.RWMutex
	m  map[Cmd]Handler
}

// NewServeMux allocates and returns a new ServeMux.
func NewServeMux() *ServeMux { return new(ServeMux) }

// HandleFunc registers the handler function for the given pattern.
func (mux *ServeMux) HandleFunc(cmd Cmd, handler func(FrameWriter, *RequestFrame), middleware ...MiddlewareFunc) {
	mux.Handle(cmd, HandlerFunc(handler), middleware...)
}

// Handle registers the handler for the given pattern.
// If a handler already exists for pattern, handle panics.
func (mux *ServeMux) Handle(cmd Cmd, handler Handler, middleware ...MiddlewareFunc) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	if handler == nil {
		panic("qrpc: nil handler")
	}
	if mux.m == nil {
		mux.m = make(map[Cmd]Handler)
	}
	if _, exist := mux.m[cmd]; exist {
		panic("qrpc: multiple registrations for " + string(cmd))
	}

	mux.m[cmd] = HandlerWithMW(handler, middleware...)
}

// ServeQRPC dispatches the request to the handler whose
// cmd matches the request.
func (mux *ServeMux) ServeQRPC(w FrameWriter, r *RequestFrame) {
	mux.mu.RLock()
	h, ok := mux.m[r.Cmd]
	if !ok {
		l.Error("cmd not registered", zap.Uint32("cmd", uint32(r.Cmd)))
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
	mu           sync.Mutex
	listeners    []net.Listener
	doneChan     chan struct{}
	shutdownFunc []func()
	done         bool

	id2Conn          []sync.Map
	activeConn       []sync.Map // for better iterate when write, map[*serveconn]struct{}
	throttle         []atomic.Value
	closeRateLimiter []ratelimit.Limiter

	wg sync.WaitGroup // wait group for goroutines

	pushID uint64
}

type throttle struct {
	on bool
	ch chan struct{}
}

// NewServer creates a server
func NewServer(bindings []ServerBinding) *Server {
	closeRateLimiter := make([]ratelimit.Limiter, len(bindings))
	for idx, binding := range bindings {
		if binding.MaxCloseRate != 0 {
			closeRateLimiter[idx] = ratelimit.New(binding.MaxCloseRate)
		}
		if binding.WriteFrameChSize < 1 {
			// at least 1 for WriteFrameChSize
			bindings[idx].WriteFrameChSize = 1
		}
	}
	return &Server{
		bindings:         bindings,
		upTime:           time.Now(),
		listeners:        make([]net.Listener, len(bindings)),
		doneChan:         make(chan struct{}),
		id2Conn:          make([]sync.Map, len(bindings)),
		activeConn:       make([]sync.Map, len(bindings)),
		throttle:         make([]atomic.Value, len(bindings)),
		closeRateLimiter: closeRateLimiter,
	}
}

// ListenAndServe starts listening on all bindings
func (srv *Server) ListenAndServe() (err error) {

	err = srv.ListenAll()
	if err != nil {
		return
	}
	return srv.ServeAll()
}

const (
	// DefaultKeepAliveDuration for keep alive duration
	// TODO make it configurable
	DefaultKeepAliveDuration = 20 * time.Second
)

// ListenAll for listen on all bindings
func (srv *Server) ListenAll() (err error) {

	for i, binding := range srv.bindings {

		var ln net.Listener

		if binding.ListenFunc != nil {
			ln, err = binding.ListenFunc("tcp", binding.Addr)
		} else {
			ln, err = net.Listen("tcp", binding.Addr)
		}
		if err != nil {
			return
		}

		kalConf := KeepAliveListenerConfig{
			KeepAliveDuration: DefaultKeepAliveDuration,
			WriteBufferSize:   binding.WBufSize,
			ReadBufferSize:    binding.RBufSize,
		}
		kal := TCPKeepAliveListener{
			Listener: ln,
			Conf:     kalConf}

		if binding.OverlayNetwork != nil {
			srv.bindings[i].ln = binding.OverlayNetwork(&kal, srv.bindings[i].TLSConf)
		} else {

			if srv.bindings[i].TLSConf != nil {
				srv.bindings[i].ln = &TLSKeepAliveListener{
					TCPKeepAliveListener: kal,
					TLSConfig:            srv.bindings[i].TLSConf,
				}
			} else {
				srv.bindings[i].ln = &kal
			}

		}
	}

	return
}

// BindingConfig for retrieve ServerBinding
func (srv *Server) BindingConfig(idx int) ServerBinding {
	return srv.bindings[idx]
}

// ServeAll for serve on all bindings
func (srv *Server) ServeAll() error {
	var g run.Group

	for i := range srv.bindings {
		idx := i
		binding := srv.bindings[i]
		g.Add(func() error {
			return srv.Serve(binding.ln, idx)
		}, func(err error) {
			serr := srv.Shutdown()
			l.Error("Shutdown", zap.Error(err), zap.Error(serr))
		})
	}

	return g.Run()
}

// Listener defines required listener methods for qrpc
type Listener interface {
	net.Listener
}

var (

	// ErrServerClosed is returned by the Server's Serve, ListenAndServe,
	// methods after a call to Shutdown or Close.
	ErrServerClosed = errors.New("qrpc: Server closed")
	// ErrListenerAcceptReturnType when Listener.Accept doesn't return TCPConn
	ErrListenerAcceptReturnType = errors.New("qrpc: Listener.Accept doesn't return TCPConn")
)

// Serve accepts incoming connections on the Listener ln, creating a
// new service goroutine for each. The service goroutines read requests and
// then call srv.Handler to reply to them.
//
// Serve always returns a non-nil error. After Shutdown or Close, the
// returned error is ErrServerClosed.
func (srv *Server) Serve(ln Listener, idx int) error {

	defer ln.Close()
	var tempDelay time.Duration // how long to sleep on accept failure

	srv.trackListener(ln, idx, true)
	defer srv.trackListener(ln, idx, false)

	serveCtx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	for {
		srv.waitThrottle(idx, srv.doneChan)
		rw, e := ln.Accept()
		if e != nil {
			select {
			case <-srv.doneChan:
				return ErrServerClosed
			default:
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
				l.Error("qrpc: Accept", zap.Duration("retrying in", tempDelay), zap.Error(e))
				time.Sleep(tempDelay)
				continue
			}
			l.Error("qrpc: Accept fatal", zap.Error(e)) // accept4: too many open files in system
			time.Sleep(time.Second)                     // keep trying instead of quit
			continue
		}
		tempDelay = 0

		GoFunc(&srv.wg, func() {
			c := srv.newConn(serveCtx, rw, idx)
			c.serve()
		})
	}
}

// TCPKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections.
type TCPKeepAliveListener struct {
	Listener
	Conf KeepAliveListenerConfig
}

// TCPConn in qrpc's aspect
type TCPConn interface {
	net.Conn
	SetKeepAlive(keepalive bool) error
	SetKeepAlivePeriod(d time.Duration) error
	SetWriteBuffer(bytes int) error
	SetReadBuffer(bytes int) error
}

// TLSKeepAliveListener for methods in the set of TCPConn-net.Conn
type TLSKeepAliveListener struct {
	TCPKeepAliveListener // embed directly for better locality
	TLSConfig            *tls.Config
}

// Accept returns a tls wrapped net.Conn
func (tlsln *TLSKeepAliveListener) Accept() (c net.Conn, err error) {
	c, err = tlsln.TCPKeepAliveListener.Accept()
	if err != nil {
		return
	}

	c = tls.Server(c, tlsln.TLSConfig)
	return
}

// Accept returns a keepalived net.Conn
func (ln *TCPKeepAliveListener) Accept() (c net.Conn, err error) {
	c, err = ln.Listener.Accept()
	if err != nil {
		return
	}

	var (
		tc TCPConn
		ok bool
	)
	if tc, ok = c.(TCPConn); !ok {
		err = ErrListenerAcceptReturnType
		return
	}

	if ln.Conf.KeepAliveDuration > 0 {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(ln.Conf.KeepAliveDuration)
	}
	if ln.Conf.WriteBufferSize > 0 {
		sockOptErr := tc.SetWriteBuffer(ln.Conf.WriteBufferSize)
		if sockOptErr != nil {
			l.Error("SetWriteBuffer", zap.Int("wbufSize", ln.Conf.WriteBufferSize), zap.Error(sockOptErr))
		}
	}
	if ln.Conf.ReadBufferSize > 0 {
		sockOptErr := tc.SetReadBuffer(ln.Conf.ReadBufferSize)
		if sockOptErr != nil {
			l.Error("SetReadBuffer", zap.Int("rbufSize", ln.Conf.ReadBufferSize), zap.Error(sockOptErr))
		}
	}

	return
}

func (srv *Server) trackListener(ln net.Listener, idx int, add bool) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if add {
		srv.listeners[idx] = ln
	} else {
		srv.listeners[idx] = nil
	}
}

// Create new connection from rwc.
func (srv *Server) newConn(ctx context.Context, rwc net.Conn, idx int) (sc *serveconn) {
	sc = &serveconn{
		server:         srv,
		rwc:            rwc,
		idx:            idx,
		untrackedCh:    make(chan struct{}),
		cs:             &ConnStreams{},
		readFrameCh:    make(chan readFrameResult, srv.bindings[idx].ReadFrameChSize),
		writeFrameCh:   make(chan *writeFrameRequest, srv.bindings[idx].WriteFrameChSize),
		cachedRequests: make([]*writeFrameRequest, 0, srv.bindings[idx].WriteFrameChSize),
		cachedBuffs:    make(net.Buffers, 0, srv.bindings[idx].WriteFrameChSize),
		wlockCh:        make(chan struct{}, 1)}

	ctx, cancelCtx := context.WithCancel(ctx)
	ctx = context.WithValue(ctx, ConnectionInfoKey, &ConnectionInfo{serveconn: sc})

	sc.cancelCtx = cancelCtx
	sc.ctx = ctx
	sc.bytesWriter = NewWriterWithTimeout(ctx, rwc, srv.bindings[idx].DefaultWriteTimeout)

	srv.activeConn[idx].Store(sc, struct{}{})

	return sc
}

var kickOrder uint64

// bindID bind the id to sc
// it is concurrent safe
func (srv *Server) bindID(sc *serveconn, id string) (kick bool, ko uint64) {

	idx := sc.idx

check:
	v, loaded := srv.id2Conn[idx].LoadOrStore(id, sc)

	if loaded {
		vsc := v.(*serveconn)
		if vsc == sc {
			return
		}
		ok, ch := srv.untrack(vsc, true)
		if !ok {
			<-ch
		}
		l.Debug("trigger closeUntracked", zap.Uintptr("sc", uintptr(unsafe.Pointer(sc))), zap.Uintptr("vsc", uintptr(unsafe.Pointer(vsc))))

		err := vsc.closeUntracked()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok {
				err = opErr.Err
			}
		}

		if srv.bindings[idx].CounterMetric != nil {
			errStr := fmt.Sprintf("%v", err)
			countlvs := []string{"method", "kickoff", "error", errStr}
			srv.bindings[idx].CounterMetric.With(countlvs...).Add(1)
		}

		atomic.AddUint64(&kickOrder, 1)
		kick = true

		goto check
	}

	ko = atomic.LoadUint64(&kickOrder)
	return
}

func (srv *Server) untrack(sc *serveconn, kicked bool) (bool, <-chan struct{}) {

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

	if kicked {
		if srv.bindings[idx].OnKickCB != nil {
			srv.bindings[idx].OnKickCB(sc.GetWriter())
		}
	}
	close(sc.untrackedCh)
	return true, sc.untrackedCh
}

var shutdownPollInterval = 500 * time.Millisecond

// Shutdown gracefully shutdown the server
func (srv *Server) Shutdown() error {

	srv.mu.Lock()
	if srv.done {
		srv.mu.Unlock()
		goto done
	}

	{
		lnerr := srv.closeListenersLocked()
		if lnerr != nil {
			srv.mu.Unlock()
			return lnerr
		}
	}

	srv.done = true
	srv.mu.Unlock()

	close(srv.doneChan)

	for _, f := range srv.shutdownFunc {
		f()
	}

done:
	srv.wg.Wait()

	return nil
}

// OnShutdown registers f to be called when shutdown
func (srv *Server) OnShutdown(f func()) {

	srv.mu.Lock()
	if srv.done {
		srv.mu.Unlock()
		f()
		return
	}

	srv.shutdownFunc = append(srv.shutdownFunc, f)
	srv.mu.Unlock()

}

// GetPushID gets the pushId
func (srv *Server) GetPushID() uint64 {
	pushID := atomic.AddUint64(&srv.pushID, 1)
	return pushID
}

// WalkConnByID iterates over  serveconn by ids
func (srv *Server) WalkConnByID(idx int, ids []string, f func(FrameWriter, *ConnectionInfo, int)) {
	for i, id := range ids {
		v, ok := srv.id2Conn[idx].Load(id)
		if ok {
			sc := v.(*serveconn)
			f(v.(*serveconn).GetWriter(), sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo), i)
		}
	}
}

// GetConnectionInfoByID returns the ConnectionInfo for idx+id
func (srv *Server) GetConnectionInfoByID(idx int, id string) *ConnectionInfo {
	v, ok := srv.id2Conn[idx].Load(id)
	if !ok {
		return nil
	}

	return v.(*serveconn).ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
}

// WalkConn walks through each serveconn
func (srv *Server) WalkConn(idx int, f func(FrameWriter, *ConnectionInfo) bool) {
	srv.activeConn[idx].Range(func(k, v interface{}) bool {
		sc := k.(*serveconn)
		return f(sc.GetWriter(), sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo))
	})
}

func (srv *Server) closeListenersLocked() (err error) {
	for idx, ln := range srv.listeners {
		if ln == nil {
			continue
		}
		if err = ln.Close(); err != nil {
			return
		}
		srv.listeners[idx] = nil
	}
	return
}

// waitThrottle is concurrent safe
func (srv *Server) waitThrottle(idx int, doneCh <-chan struct{}) {
	v := srv.throttle[idx].Load()
	t, ok := v.(throttle)
	if ok && t.on {
		select {
		case <-t.ch:
		case <-doneCh:
		}
	}
}

// SetThrottle sets throttle on
func (srv *Server) SetThrottle(idx int) {
	v := srv.throttle[idx].Load()
	if v != nil {
		// already on,do nothing
		if v.(throttle).on {
			return
		}
	}
	srv.throttle[idx].Store(throttle{on: true, ch: make(chan struct{})})
}

// ClearThrottle clears throttle onff
func (srv *Server) ClearThrottle(idx int) {
	v := srv.throttle[idx].Load()
	if v == nil {
		return
	}
	close(v.(throttle).ch)

	srv.throttle[idx].Store(throttle{on: false, ch: make(chan struct{})})
}
