package qrpc

import (
	"context"
	"crypto/tls"
	"errors"
	mathrand "math/rand"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"go.uber.org/zap"
)

const (
	reconnectIntervalAfter1stRound = time.Second * 2
)

// Connection defines a qrpc connection
// it is thread safe
type Connection struct {
	// immutable
	addrs      []string
	reconnect  bool
	conf       ConnectionConfig
	subscriber SubFunc // there can be only one subscriber because of streamed frames

	writeFrameCh chan *writeFrameRequest // it's never closed so won't panic

	idx int // modified in connect

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc
	// ctx is the corresponding context for cancelCtx
	ctx context.Context
	wg  sync.WaitGroup // wait group for goroutines

	// access by atomic
	closed uint32
	ridGen uint64

	// access by mutex
	mu              sync.Mutex
	rwc             net.Conn
	respes          map[uint64]*response
	loopCtx         *context.Context
	loopCancelCtx   context.CancelFunc
	loopBytesWriter *Writer
	loopWG          *sync.WaitGroup

	cachedRequests []*writeFrameRequest
	cachedBuffs    net.Buffers

	cs *ConnStreams
}

// Response for response frames
type Response interface {
	GetFrame() (*Frame, error)
	GetFrameWithContext(ctx context.Context) (*Frame, error) // frame is valid is error is nil
}

type response struct {
	Frame chan *Frame
}

func (r *response) GetFrame() (*Frame, error) {
	frame := <-r.Frame
	if frame == nil {
		return nil, ErrConnAlreadyClosed
	}
	return frame, nil
}

func (r *response) GetFrameWithContext(ctx context.Context) (*Frame, error) {
	select {
	case frame := <-r.Frame:
		if frame == nil {
			return nil, ErrConnAlreadyClosed
		}
		return frame, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *response) SetResponse(frame *Frame) {
	r.Frame <- frame
}

func (r *response) Close() {
	close(r.Frame)
}

// NewConnection constructs a *Connection without reconnect ability
func NewConnection(addr string, conf ConnectionConfig, f SubFunc) (conn *Connection, err error) {
	var rwc net.Conn
	if conf.OverlayNetwork != nil {
		rwc, err = conf.OverlayNetwork(addr, DialConfig{DialTimeout: conf.DialTimeout, WBufSize: conf.WBufSize, RBufSize: conf.RBufSize, TLSConf: conf.TLSConf})
	} else {
		rwc, err = dialTCP(addr, DialConfig{DialTimeout: conf.DialTimeout, WBufSize: conf.WBufSize, RBufSize: conf.RBufSize, TLSConf: conf.TLSConf})
	}

	if err != nil {
		l.Error("NewConnection Dial", zap.Error(err))
		return
	}

	conn = newConnection(rwc, []string{addr}, conf, f, false)
	return
}

func dialTCP(addr string, dialConfig DialConfig) (rwc net.Conn, err error) {
	rwc, err = net.DialTimeout("tcp", addr, dialConfig.DialTimeout)
	if err != nil {
		l.Error("dialTCP addr", zap.String("addr", addr), zap.Error(err))
		return
	}

	tc := rwc.(*net.TCPConn)
	if dialConfig.RBufSize > 0 {
		sockOptErr := tc.SetReadBuffer(dialConfig.RBufSize)
		if sockOptErr != nil {
			l.Error("SetReadBuffer", zap.Int("RBufSize", dialConfig.RBufSize), zap.Error(sockOptErr))
		}
	}
	if dialConfig.WBufSize > 0 {
		sockOptErr := tc.SetWriteBuffer(dialConfig.WBufSize)
		if sockOptErr != nil {
			l.Error("SetWriteBuffer", zap.Int("WBufSize", dialConfig.WBufSize), zap.Error(sockOptErr))
		}
	}

	if dialConfig.TLSConf != nil {
		tlsConn := tls.Client(rwc, dialConfig.TLSConf)
		err = tlsConn.Handshake()
		if err != nil {
			rwc.Close()
			return
		}

		rwc = tlsConn
	}

	return
}

// NewConnectionWithReconnect constructs a *Connection with reconnect ability
func NewConnectionWithReconnect(addrs []string, conf ConnectionConfig, f SubFunc) *Connection {
	var copy []string
	for _, addr := range addrs {
		copy = append(copy, addr)
	}
	mathrand.Shuffle(len(copy), func(i, j int) {
		copy[i], copy[j] = copy[j], copy[i]
	})

	var (
		rwc net.Conn
		err error
	)
	if conf.OverlayNetwork != nil {
		rwc, err = conf.OverlayNetwork(copy[len(copy)-1], DialConfig{DialTimeout: conf.DialTimeout, WBufSize: conf.WBufSize, RBufSize: conf.RBufSize})
	} else {
		rwc, err = dialTCP(copy[len(copy)-1], DialConfig{DialTimeout: conf.DialTimeout, WBufSize: conf.WBufSize, RBufSize: conf.RBufSize})
	}
	if err != nil {
		l.Error("initconnect DialTimeout", zap.Error(err))
		rwc = nil
	}
	return newConnection(rwc, copy, conf, f, true)
}

// ClientConnectionInfoKey is context key for ClientConnectionInfo
// used to store custom information
var ClientConnectionInfoKey = &contextKey{"qrpc-clientconnection"}

// ClientConnectionInfo for store info on clientconnection
type ClientConnectionInfo struct {
	CC *Connection
}

func newConnection(rwc net.Conn, addr []string, conf ConnectionConfig, f SubFunc, reconnect bool) *Connection {
	ctx, cancelCtx := context.WithCancel(context.Background())

	c := &Connection{
		rwc: rwc, addrs: addr, conf: conf, subscriber: f,
		writeFrameCh: make(chan *writeFrameRequest, conf.WriteFrameChSize), respes: make(map[uint64]*response),
		cachedRequests: make([]*writeFrameRequest, 0, conf.WriteFrameChSize),
		cachedBuffs:    make(net.Buffers, 0, conf.WriteFrameChSize),
		cs:             &ConnStreams{}, ctx: ctx, cancelCtx: cancelCtx,
		reconnect: reconnect}

	if conf.Handler != nil {
		c.ctx = context.WithValue(c.ctx, ClientConnectionInfoKey, &ClientConnectionInfo{CC: c})
	}

	if rwc != nil {
		// loopxxx should be paired with rwc
		loopCtx, loopCancelCtx := context.WithCancel(ctx)
		c.loopCancelCtx = loopCancelCtx
		atomic.StorePointer((*unsafe.Pointer)((unsafe.Pointer)(&c.loopCtx)), unsafe.Pointer(&loopCtx))
		c.loopBytesWriter = NewWriterWithTimeout(loopCtx, rwc, conf.WriteTimeout)
	}
	GoFunc(&c.wg, c.loop)
	return c
}

func (conn *Connection) loop() {
	for {
		if err := conn.connect(); err != nil {
			// connx.Close() was called
			return
		}

		conn.loopWG = &sync.WaitGroup{}
		GoFunc(conn.loopWG, func() {
			conn.readFrames()
		})

		GoFunc(conn.loopWG, func() {
			conn.writeFrames()
		})

		<-(*conn.loopCtx).Done()
		conn.closeRWC()
		conn.loopWG.Wait()
		conn.endLoop()

		// close & quit if not reconnect; otherwise automatically reconnect
		if !conn.reconnect {
			conn.Close()
			return
		}
	}
}

func (conn *Connection) atomicLoopCtx() context.Context {
	return *(*context.Context)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&conn.loopCtx))))
}

func (conn *Connection) connect() error {
	// directly return if connection already established
	if conn.rwc != nil {
		return nil
	}

	count := 0
	for {
		addr := conn.addrs[conn.idx%len(conn.addrs)]
		conn.idx++
		count++

		var (
			rwc net.Conn
			err error
		)
		if conn.conf.OverlayNetwork != nil {
			rwc, err = conn.conf.OverlayNetwork(addr, DialConfig{DialTimeout: conn.conf.DialTimeout, WBufSize: conn.conf.WBufSize, RBufSize: conn.conf.RBufSize})
		} else {
			rwc, err = dialTCP(addr, DialConfig{DialTimeout: conn.conf.DialTimeout, WBufSize: conn.conf.WBufSize, RBufSize: conn.conf.RBufSize})
		}

		if err != nil {
			l.Error("connect DialTimeout", zap.Error(err))
		} else {
			ctx, cancelCtx := context.WithCancel(conn.ctx)
			conn.mu.Lock()
			conn.rwc = rwc
			atomic.StorePointer((*unsafe.Pointer)((unsafe.Pointer)(&conn.loopCtx)), unsafe.Pointer(&ctx))
			conn.loopCancelCtx = cancelCtx
			conn.loopBytesWriter = NewWriterWithTimeout(ctx, rwc, conn.conf.WriteTimeout)
			conn.mu.Unlock()
			return nil
		}

		if count >= len(conn.addrs) {
			time.Sleep(reconnectIntervalAfter1stRound)
		}

		select {
		case <-conn.ctx.Done():
			return conn.ctx.Err()
		default:
		}
	}

}

// Wait block until closed
func (conn *Connection) Wait() {
	conn.wg.Wait()
}

// StreamRequest is for streamed request
func (conn *Connection) StreamRequest(cmd Cmd, flags FrameFlag, payload []byte) (StreamWriter, Response, error) {

	flags = flags.ToStream()
	_, resp, writer, err := conn.writeFirstFrame(cmd, flags, payload)
	if err != nil {
		l.Error("writeFirstFrame", zap.Error(err))
		return nil, nil, err
	}
	return (*defaultStreamWriter)(writer), resp, nil
}

// Request send a nonstreamed request frame and returns response frame
// error is non nil when write failed
func (conn *Connection) Request(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, error) {

	flags = flags.ToNonStream()
	requestID, resp, _, err := conn.writeFirstFrame(cmd, flags, payload)

	return requestID, resp, err
}

var (
	// ErrNoNewUUID when no new uuid available
	ErrNoNewUUID = errors.New("no new uuid available temporary")
	// ErrConnAlreadyClosed when try to operate on an already closed conn
	ErrConnAlreadyClosed = errors.New("connection already closed")
)

func (conn *Connection) nextRequestID() uint64 {
	ridGen := atomic.AddUint64(&conn.ridGen, 1)
	return 2*ridGen + 1
}

func (conn *Connection) writeFirstFrame(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, *defaultFrameWriter, error) {

	if conn.IsClosed() {
		return 0, nil, nil, ErrConnAlreadyClosed
	}

	resp := &response{Frame: make(chan *Frame, 1)}
	requestID := conn.nextRequestID()

	writer := newFrameWriter(conn)
	writer.resp = resp
	writer.StartWrite(requestID, cmd, flags)
	writer.WriteBytes(payload)
	err := writer.EndWrite()

	if err != nil {
		return 0, nil, nil, err
	}

	return requestID, resp, writer, nil
}

func (conn *Connection) getCodec() CompressorCodec {
	return conn.conf.Codec
}

func (conn *Connection) writeFrameBytes(dfw *defaultFrameWriter) error {

	wfr := wfrPool.Get().(*writeFrameRequest)
	wfr.dfw = dfw
	// just in case
	if len(wfr.result) > 0 {
		<-wfr.result
	}

	loopCtx := conn.atomicLoopCtx()
	select {
	case conn.writeFrameCh <- wfr:
	case <-loopCtx.Done():
		return loopCtx.Err()
	}

	select {
	case err := <-wfr.result:
		wfr.dfw = nil
		wfrPool.Put(wfr)
		return err
	case <-loopCtx.Done():
		return loopCtx.Err()
	}
}

// ResetFrame resets a stream by requestID
func (conn *Connection) ResetFrame(requestID uint64, reason Cmd) error {
	w := conn.getWriter()
	err := w.ResetFrame(requestID, reason)
	w.Finalize()
	return err
}

// close current rwc
func (conn *Connection) closeRWC() {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.rwc == nil {
		return
	}
	conn.rwc.Close()
	conn.rwc = nil // so that connect will Dial another rwc

	for _, v := range conn.respes {
		v.Close()
	}
	conn.respes = make(map[uint64]*response)
}

// endLoop is called after read/write g finishes, so no need for lock
func (conn *Connection) endLoop() {

	conn.cs.Release()
	conn.cs = &ConnStreams{}

}

// Close closes the qrpc connection
func (conn *Connection) Close() error {

	swapped := atomic.CompareAndSwapUint32(&conn.closed, 0, 1)

	if !swapped {
		return ErrConnAlreadyClosed
	}

	conn.cancelCtx()

	return nil
}

// Done returns the done channel
func (conn *Connection) Done() <-chan struct{} {
	return conn.ctx.Done()
}

// IsClosed tells whether connection is closed
func (conn *Connection) IsClosed() bool {
	return atomic.LoadUint32(&conn.closed) == 1
}

func (conn *Connection) readFrames() {

	defer conn.loopCancelCtx()

	// in case closeRWC is already called
	if conn.rwc == nil {
		return
	}
	reader := newFrameReader(*conn.loopCtx, conn.rwc, conn.conf.ReadTimeout, conn.conf.Codec)
	defer reader.Finalize()

	for {
		frame, err := reader.ReadFrame(conn.cs)
		if err != nil {
			return
		}

		if frame.Flags.IsPush() {
			// pushed frame
			if conn.subscriber != nil {
				conn.subscriber(conn, frame)
			}

			continue
		}

		// deal with pulled frames
		conn.mu.Lock()
		resp, ok := conn.respes[frame.RequestID]
		if !ok {
			rwcClosed := conn.rwc == nil
			conn.mu.Unlock()
			if rwcClosed {
				// order: 1. got a frame 2. closeRWC 3.!ok
				// just ignore it
				return
			}
			if conn.conf.Handler != nil {
				if !frame.FromServer() {
					l.Error("clientconn get RequestFrame.RequestID not even")
					return
				}
				if !frame.Flags.IsNonBlock() {
					l.Error("clientconn get RequestFrame block")
					return
				}
				GoFunc(conn.loopWG, func() {
					defer conn.handleRequestPanic((*RequestFrame)(frame), time.Now())
					w := conn.getWriter()
					conn.conf.Handler.ServeQRPC(w, (*RequestFrame)(frame))
					w.Finalize()
				})
				continue
			}

			l.Error("dangling resp", zap.Uint64("requestID", frame.RequestID))

			continue
		}
		delete(conn.respes, frame.RequestID)
		conn.mu.Unlock()

		resp.SetResponse(frame)

	}
}

func (conn *Connection) handleRequestPanic(frame *RequestFrame, begin time.Time) {
	err := recover()
	if err != nil {
		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
		l.Error("Connection.handleRequestPanic", zap.String("stack", String(buf)), zap.Any("err", err))
	}

	s := frame.Stream
	if !s.IsSelfClosed() {
		// send error frame
		w := conn.getWriter()
		w.StartWrite(frame.RequestID, 0, StreamRstFlag)
		err := w.EndWrite()
		if err != nil {
			l.Debug("Connection.send error frame", zap.Any("frame", frame), zap.Error(err))
		}
		w.Finalize()
	}
}

func (conn *Connection) getWriter() *defaultFrameWriter {
	return newFrameWriter(conn)
}

func (conn *Connection) writeFrames() (err error) {

	defer conn.loopCancelCtx()

	batch := conn.conf.WriteFrameChSize
	for {
		conn.cachedRequests = conn.cachedRequests[:0]
		conn.cachedBuffs = conn.cachedBuffs[:0]

		err = conn.collectWriteFrames(batch)
		if err != nil {
			return
		}
		err = conn.writeBuffers()
		if err != nil {
			return
		}
	}
}

// the returned err will be non-nil only if ctx is canceled
func (conn *Connection) collectWriteFrames(batch int) error {

	var (
		res       *writeFrameRequest
		dfw       *defaultFrameWriter
		flags     FrameFlag
		requestID uint64
	)

	// called from dedicated wg
	// wait for first request

firstFrame:
	select {
	case res = <-conn.writeFrameCh:
		dfw = res.dfw
		flags = dfw.Flags()
		requestID = dfw.RequestID()

		if flags.IsRst() {
			s := conn.cs.GetStream(requestID, flags)
			if s == nil {
				res.result <- ErrRstNonExistingStream
				goto firstFrame
			}
			// for rst frame, AddOutFrame returns false when no need to send the frame
			if !s.AddOutFrame(requestID, flags) {
				res.result <- nil
				goto firstFrame
			}
		} else if !flags.IsPush() { // skip stream logic if PushFlag set
			s, loaded := conn.cs.CreateOrGetStream(*conn.loopCtx, requestID, flags)
			if !loaded {
				l.Debug("serveconn new stream", zap.Uint64("requestID", requestID), zap.Uint8("flags", uint8(flags)), zap.Uint32("cmd", uint32(dfw.Cmd())))
			}
			if !s.AddOutFrame(requestID, flags) {
				res.result <- ErrWriteAfterCloseSelf
				goto firstFrame
			}
		}

		conn.cachedRequests = append(conn.cachedRequests, res)
		conn.cachedBuffs = append(conn.cachedBuffs, dfw.GetWbuf())

	case <-(*conn.loopCtx).Done():
		// no need to deal with sc.cachedRequests since they will fall into the same case anyway
		return (*conn.loopCtx).Err()
	}

	batch--
	if batch <= 0 {
		return nil
	}

	for i := 0; i < batch; i++ {
		select {
		case res = <-conn.writeFrameCh:
			dfw = res.dfw
			flags = dfw.Flags()
			requestID = dfw.RequestID()

			if flags.IsRst() {
				s := conn.cs.GetStream(requestID, flags)
				if s == nil {
					res.result <- ErrRstNonExistingStream
					break
				}
				// for rst frame, AddOutFrame returns false when no need to send the frame
				if !s.AddOutFrame(requestID, flags) {
					res.result <- nil
					break
				}
			} else if !flags.IsPush() { // skip stream logic if PushFlag set
				s, loaded := conn.cs.CreateOrGetStream(*conn.loopCtx, requestID, flags)
				if !loaded {
					l.Debug("serveconn new stream", zap.Uint64("requestID", requestID), zap.Uint8("flags", uint8(flags)), zap.Uint32("cmd", uint32(dfw.Cmd())))
				}
				if !s.AddOutFrame(requestID, flags) {
					res.result <- ErrWriteAfterCloseSelf
					break
				}
			}

			conn.cachedRequests = append(conn.cachedRequests, res)
			conn.cachedBuffs = append(conn.cachedBuffs, dfw.GetWbuf())

		case <-(*conn.loopCtx).Done():
			// no need to deal with sc.cachedRequests since they will fall into the same case anyway
			return (*conn.loopCtx).Err()
		default:
			return nil
		}
	}

	return nil
}

func (conn *Connection) writeBuffers() error {
	if len(conn.cachedRequests) == 0 {
		// nothing to do
		return nil
	}

	// must prepare respes before actually write
	conn.mu.Lock()
	if conn.rwc == nil {
		conn.mu.Unlock()
		for _, request := range conn.cachedRequests {
			request.result <- ErrConnAlreadyClosed
		}
		return ErrConnAlreadyClosed
	}
	for idx, request := range conn.cachedRequests {
		if request.dfw.resp != nil {
			requestID := request.dfw.RequestID()
			if conn.respes[requestID] != nil {
				request.result <- ErrNoNewUUID
				request.result = nil
				conn.cachedBuffs[idx] = nil
				continue
			}
			conn.respes[requestID] = request.dfw.resp
			request.dfw.resp = nil
		}
	}
	conn.mu.Unlock()

	cachedBuffs := conn.cachedBuffs
	_, err := conn.loopBytesWriter.writeBuffers(&conn.cachedBuffs)
	conn.cachedBuffs = cachedBuffs
	if err != nil {
		l.Error("Connection.writeBuffers", zap.Uintptr("conn", uintptr(unsafe.Pointer(conn))), zap.Error(err))

		if opErr, ok := err.(*net.OpError); ok {
			err = opErr.Err
		}
		for idx, request := range conn.cachedRequests {
			if len(conn.cachedBuffs[idx]) != 0 {
				request.result <- err
			} else {
				if request.result != nil {
					request.result <- nil
				}
			}
		}
		return err
	}
	for _, request := range conn.cachedRequests {
		if request.result != nil {
			request.result <- nil
		}
	}

	return nil
}
