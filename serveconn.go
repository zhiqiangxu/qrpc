package qrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"go.uber.org/zap"
)

const (
	// DefaultMaxFrameSize is the max size for each request frame
	DefaultMaxFrameSize = 10 * 1024 * 1024
)

// A serveconn represents the server side of an qrpc connection.
// all fields (except untrack) are immutable, mutables are in ConnectionInfo
type serveconn struct {
	// server is the server on which the connection arrived.
	server *Server

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc
	// ctx is the corresponding context for cancelCtx
	ctx context.Context
	wg  sync.WaitGroup // wait group for goroutines

	idx int

	cs *ConnStreams

	// rwc is the underlying network connection.
	// This is never wrapped by other types and is the value given out
	// to CloseNotifier callers. It is usually of type *net.TCPConn
	rwc net.Conn

	reader         *defaultFrameReader // used in conn.readFrames
	writer         FrameWriter         // used by handlers
	bytesWriter    *Writer
	readFrameCh    chan readFrameResult    // written by conn.readFrames
	writeFrameCh   chan *writeFrameRequest // written by FrameWriter
	inflight       int32
	ridGen         uint64
	wlockCh        chan struct{}
	cachedRequests []*writeFrameRequest
	cachedBuffs    net.Buffers

	// modified by Server
	untrack     uint32 // ony the first call to untrack actually do it, subsequent calls should wait for untrackedCh
	untrackedCh chan struct{}
}

// ConnectionInfoKey is context key for ConnectionInfo
// used to store custom information
var ConnectionInfoKey = &contextKey{"qrpc-connection"}

// ConnectionInfo for store info on connection
type ConnectionInfo struct {
	*serveconn
	l           sync.RWMutex
	closed      bool
	id          string
	closeNotify []func()
	anything    interface{}
	respes      map[uint64]*response
}

// GetAnything returns anything
func (ci *ConnectionInfo) GetAnything() interface{} {
	ci.l.RLock()
	defer ci.l.RUnlock()
	return ci.anything
}

// SetAnything sets anything
func (ci *ConnectionInfo) SetAnything(anything interface{}) {
	ci.l.Lock()
	ci.anything = anything
	ci.l.Unlock()
}

// GetID returns the ID
func (ci *ConnectionInfo) GetID() string {
	ci.l.RLock()
	defer ci.l.RUnlock()
	return ci.id
}

// SetID sets id and kicks previous id if exists
func (ci *ConnectionInfo) SetID(id string) (bool, uint64) {
	if id == "" {
		panic("empty id not allowed")
	}
	ci.l.Lock()
	if ci.id != "" {
		ci.l.Unlock()
		panic("SetID called twice")
	}
	ci.id = id
	ci.l.Unlock()

	return ci.serveconn.server.bindID(ci.serveconn, id)
}

// ReaderConfig for change reader timeout
type ReaderConfig interface {
	SetReadTimeout(timeout int)
}

// ReaderConfig for change reader config
func (ci *ConnectionInfo) ReaderConfig() ReaderConfig {
	return ci.serveconn.reader
}

// NotifyWhenClose ensures f is called when connection is closed
func (ci *ConnectionInfo) NotifyWhenClose(f func()) {
	ci.l.Lock()

	if ci.closed {
		ci.l.Unlock()
		f()
		return
	}

	ci.closeNotify = append(ci.closeNotify, f)
	ci.l.Unlock()
}

// Server returns the server
func (sc *serveconn) Server() *Server {
	return sc.server
}

func (sc *serveconn) RemoteAddr() string {
	return sc.rwc.RemoteAddr().String()
}

// Serve a new connection.
func (sc *serveconn) serve() {

	idx := sc.idx

	defer func() {
		// connection level panic
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			l.Error("connection panic", zap.String("ip", sc.RemoteAddr()), zap.String("stack", String(buf)), zap.Any("err", err))
		}
		sc.Close()
		sc.wg.Wait()
	}()

	binding := sc.server.bindings[idx]
	var maxFrameSize int
	if binding.MaxFrameSize > 0 {
		maxFrameSize = binding.MaxFrameSize
	} else {
		maxFrameSize = DefaultMaxFrameSize
	}
	ctx := sc.ctx
	sc.reader = newFrameReaderWithMFS(ctx, sc.rwc, binding.DefaultReadTimeout, binding.Codec, maxFrameSize)
	sc.writer = newFrameWriter(sc) // only used by blocking mode

	sc.inflight = 1
	GoFunc(&sc.wg, func() {
		sc.readFrames()
	})

	handler := binding.Handler

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-sc.readFrameCh:

			if res.readMore != nil {
				func() {
					defer sc.handleRequestPanic(res.f, time.Now())
					handler.ServeQRPC(sc.writer, res.f)
				}()
				res.readMore()
			} else {
				GoFunc(&sc.wg, func() {
					defer sc.handleRequestPanic(res.f, time.Now())
					w := newFrameWriter(sc)
					handler.ServeQRPC(w, res.f)
					w.Finalize()
				})
			}
		}

	}

}

func (sc *serveconn) instrument(frame *RequestFrame, begin time.Time, err interface{}) {
	binding := sc.server.bindings[sc.idx]

	if binding.CounterMetric == nil && binding.LatencyMetric == nil {
		return
	}

	errStr := fmt.Sprintf("%v", err)
	if binding.CounterMetric != nil {
		countlvs := []string{"method", strconv.Itoa(int(frame.Cmd)), "error", errStr}
		binding.CounterMetric.With(countlvs...).Add(1)
	}

	if binding.LatencyMetric == nil {
		return
	}

	lvs := []string{"method", strconv.Itoa(int(frame.Cmd)), "error", errStr}

	binding.LatencyMetric.With(lvs...).Observe(time.Since(begin).Seconds())

}

func (sc *serveconn) handleRequestPanic(frame *RequestFrame, begin time.Time) {
	err := recover()
	sc.instrument(frame, begin, err)

	if err != nil {

		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
		l.Error("handleRequestPanic", zap.String("ip", sc.RemoteAddr()), zap.String("stack", String(buf)), zap.Any("err", err))

	}

	s := frame.Stream
	if !s.IsSelfClosed() {
		// send error frame
		writer := sc.GetWriter()
		writer.StartWrite(frame.RequestID, 0, StreamRstFlag)
		err := writer.EndWrite()
		if err != nil {
			l.Debug("send error frame", zap.String("ip", sc.RemoteAddr()), zap.Any("frame", frame), zap.Error(err))
		}
	}

}

func (sc *serveconn) GetID() string {
	ci := sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
	return ci.GetID()
}

// GetWriter generate a FrameWriter for the connection
func (sc *serveconn) GetWriter() FrameWriter {

	return newFrameWriter(sc)
}

var (
	// ErrInvalidPacket when packet invalid
	ErrInvalidPacket = errors.New("invalid packet")
)

type readFrameResult struct {
	f *RequestFrame // valid until readMore is called

	// readMore should be called once the consumer no longer needs or
	// retains f. After readMore, f is invalid and more frames can be
	// read.
	readMore func()
}

type writeFrameRequest struct {
	dfw    *defaultFrameWriter
	result chan error
}

// A gate lets two goroutines coordinate their activities.
type gate chan struct{}

const (
	errStrReadFramesForOverlayNetwork  = "readFrames err for OverlayNetwork"
	errStrWriteFramesForOverlayNetwork = "writeFrames err for OverlayNetwork"
)

func (g gate) Done() { g <- struct{}{} }

func (sc *serveconn) readFrames() (err error) {

	ci := sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)

	ctx := sc.ctx
	binding := sc.server.bindings[sc.idx]
	if binding.ReadFrameChSize > 0 {
		runtime.LockOSThread()
	}

	defer func() {
		sc.tryFreeStreams()

		if err == ErrFrameTooLarge {
			l.Error("ErrFrameTooLarge", zap.String("ip", sc.RemoteAddr()))
		}

		if binding.CounterMetric != nil {
			errStr := fmt.Sprintf("%v", err)
			if err != nil {
				if binding.OverlayNetwork != nil {
					l.Error("readFrames", zap.Any("type", reflect.TypeOf(err)), zap.Error(err))
					errStr = errStrReadFramesForOverlayNetwork
				}
			}

			countlvs := []string{"method", "readFrames", "error", errStr}
			binding.CounterMetric.With(countlvs...).Add(1)
		}
		if binding.ReadFrameChSize > 0 {
			runtime.UnlockOSThread()
		}
	}()
	gate := make(gate, 1)
	gateDone := gate.Done

	for {
		req, err := sc.reader.ReadFrame(sc.cs)
		if err != nil {
			sc.Close()
			sc.reader.Finalize()
			if opErr, ok := err.(*net.OpError); ok {
				return opErr.Err
			}
			return err
		}
		if req.FromServer() {
			ci.l.Lock()
			if ci.respes != nil {
				resp, ok := ci.respes[req.RequestID]
				if ok {
					delete(ci.respes, req.RequestID)
				}
				ci.l.Unlock()
				if ok {
					resp.SetResponse(req)
					continue
				}
			} else {
				ci.l.Unlock()
			}
		}

		if req.Flags.IsNonBlock() {
			select {
			case sc.readFrameCh <- readFrameResult{f: (*RequestFrame)(req)}:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		} else {
			select {
			case sc.readFrameCh <- readFrameResult{f: (*RequestFrame)(req), readMore: gateDone}:
			case <-ctx.Done():
				return ctx.Err()
			}

			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		sc.server.waitThrottle(sc.idx, ctx.Done())
	}

}

func (sc *serveconn) getCodec() CompressorCodec {
	return sc.server.bindings[sc.idx].Codec
}

var wfrPool = sync.Pool{New: func() interface{} {
	return &writeFrameRequest{result: make(chan error, 1)}
}}

func (sc *serveconn) writeFrameBytes(dfw *defaultFrameWriter) (err error) {
	wfr := wfrPool.Get().(*writeFrameRequest)
	wfr.dfw = dfw
	// just in case
	if len(wfr.result) > 0 {
		<-wfr.result
	}

	select {
	case sc.writeFrameCh <- wfr:
	case <-sc.ctx.Done():
		return sc.ctx.Err()
	}

	select {
	case sc.wlockCh <- struct{}{}:
		// only one allowed at a time

		if len(wfr.result) > 0 {
			// already handled, release lock
			<-sc.wlockCh
			err = <-wfr.result
			wfr.dfw = nil
			wfrPool.Put(wfr)
			return
		}

		// check for reader quit, don't release wlock if reader quit
		if !sc.addAndCheckInflight() {

			select {
			case <-sc.ctx.Done():
				return sc.ctx.Err()
			default:
				// read has quit, ctx must have been canceled
				panic("bug in qrpc inflight")
			}
		}

		binding := sc.server.bindings[sc.idx]
		var releaseWlock bool
		defer func() {

			sc.tryFreeStreams()

			if err != nil {
				if binding.CounterMetric != nil {
					errStr := fmt.Sprintf("%v", err)
					if binding.OverlayNetwork != nil {
						errStr = errStrWriteFramesForOverlayNetwork
					}
					countlvs := []string{"method", "writeFrames", "error", errStr}
					binding.CounterMetric.With(countlvs...).Add(1)
				}
			}

			if releaseWlock {
				<-sc.wlockCh
			}
		}()

		sc.cachedBuffs = sc.cachedBuffs[:0]
		sc.cachedRequests = sc.cachedRequests[:0]

		err = sc.collectWriteFrames(binding.WriteFrameChSize)
		if err != nil {
			l.Debug("sc.collectWriteFrames", zap.Uintptr("sc", uintptr(unsafe.Pointer(sc))), zap.Error(err))
			return
		}

		err = sc.writeBuffers()
		if err != nil {
			l.Debug("writeBuffers", zap.Uintptr("sc", uintptr(unsafe.Pointer(sc))), zap.Error(err))
		} else {
			// all write requests handled
			releaseWlock = true
		}

		err = <-wfr.result
		wfr.dfw = nil
		wfrPool.Put(wfr)
		return

	case err := <-wfr.result:
		wfr.dfw = nil
		wfrPool.Put(wfr)
		return err
	case <-sc.ctx.Done():
		return sc.ctx.Err()
	}

}

func (sc *serveconn) writeBuffers() error {
	if len(sc.cachedRequests) == 0 {
		// nothing to do
		return nil
	}

	// must prepare respes before actually write

	var targetIdx []int
	for idx, request := range sc.cachedRequests {
		if request.dfw.resp != nil {
			targetIdx = append(targetIdx, idx)
		}
	}
	if len(targetIdx) > 0 {
		ci := sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
		ci.l.Lock()
		if ci.closed {
			ci.l.Unlock()
			for _, request := range sc.cachedRequests {
				request.result <- ErrConnAlreadyClosed
			}
			return ErrConnAlreadyClosed
		}
		if ci.respes == nil {
			ci.respes = make(map[uint64]*response)
		}
		for _, idx := range targetIdx {
			request := sc.cachedRequests[idx]
			requestID := request.dfw.RequestID()
			if ci.respes[requestID] != nil {
				request.result <- ErrNoNewUUID
				request.result = nil
				sc.cachedBuffs[idx] = nil
				continue
			}
			ci.respes[requestID] = request.dfw.resp
			request.dfw.resp = nil
		}
		ci.l.Unlock()
	}

	var err error
	{
		cachedBuffs := sc.cachedBuffs
		_, err = sc.bytesWriter.writeBuffers(&sc.cachedBuffs)
		sc.cachedBuffs = cachedBuffs
	}

	if err != nil {

		l.Debug("serveconn.writeBuffers", zap.Uintptr("sc", uintptr(unsafe.Pointer(sc))), zap.Error(err))
		// don't call sc.Close while inside OnKickCB
		if !sc.IsClosed() {
			sc.Close()
		}

		if opErr, ok := err.(*net.OpError); ok {
			err = opErr.Err
		}
		for idx, request := range sc.cachedRequests {
			if len(sc.cachedBuffs[idx]) != 0 {
				request.result <- err
			} else {
				if request.result != nil {
					request.result <- nil
				}
			}
		}
		return err
	}
	for _, request := range sc.cachedRequests {
		if request.result != nil {
			request.result <- nil
		}
	}

	return nil
}

func (sc *serveconn) collectWriteFrames(batch int) error {
	var (
		res       *writeFrameRequest
		dfw       *defaultFrameWriter
		flags     FrameFlag
		requestID uint64
	)

	for i := 0; i < batch; i++ {
		select {
		case res = <-sc.writeFrameCh:
			dfw = res.dfw
			flags = dfw.Flags()
			requestID = dfw.RequestID()

			if flags.IsRst() {
				s := sc.cs.GetStream(requestID, flags)
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
				s, loaded := sc.cs.CreateOrGetStream(sc.ctx, requestID, flags)
				if !loaded {
					l.Debug("serveconn new stream", zap.Uint64("requestID", requestID), zap.Uint8("flags", uint8(flags)), zap.Uint32("cmd", uint32(dfw.Cmd())))
				}
				if !s.AddOutFrame(requestID, flags) {
					res.result <- ErrWriteAfterCloseSelf
					break
				}
			}

			sc.cachedRequests = append(sc.cachedRequests, res)
			sc.cachedBuffs = append(sc.cachedBuffs, dfw.GetWbuf())

		case <-sc.ctx.Done():
			// no need to deal with sc.cachedRequests since they will fall into the same case anyway
			return sc.ctx.Err()
		default:
			return nil
		}
	}
	return nil
}

func (sc *serveconn) addAndCheckInflight() bool {
	if atomic.AddInt32(&sc.inflight, 1) == 1 {
		atomic.AddInt32(&sc.inflight, -1)
		return false
	}
	return true
}

func (sc *serveconn) tryFreeStreams() {

	if atomic.AddInt32(&sc.inflight, -1) == 0 {
		sc.cs.Release()
	}
}

// Request clientconn from serveconn
func (sc *serveconn) Request(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, error) {
	flags = flags | NBFlag
	requestID, resp, _, err := sc.writeFirstFrame(cmd, flags, payload)

	return requestID, resp, err
}

// StreamRequest is for streamed request
func (sc *serveconn) StreamRequest(cmd Cmd, flags FrameFlag, payload []byte) (StreamWriter, Response, error) {

	flags = flags.ToStream()
	_, resp, writer, err := sc.writeFirstFrame(cmd, flags, payload)
	if err != nil {
		l.Error("StreamRequest writeFirstFrame", zap.Error(err))
		return nil, nil, err
	}
	return (*defaultStreamWriter)(writer), resp, nil
}

func (sc *serveconn) nextRequestID() uint64 {
	ridGen := atomic.AddUint64(&sc.ridGen, 1)
	return 2 * ridGen
}

func (sc *serveconn) IsClosed() bool {
	return atomic.LoadUint32(&sc.untrack) != 0
}

func (sc *serveconn) writeFirstFrame(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, *defaultFrameWriter, error) {

	if sc.IsClosed() {
		return 0, nil, nil, ErrConnAlreadyClosed
	}

	requestID := sc.nextRequestID()
	resp := &response{Frame: make(chan *Frame, 1)}

	writer := newFrameWriter(sc)
	writer.resp = resp
	writer.StartWrite(requestID, cmd, flags)
	writer.WriteBytes(payload)
	err := writer.EndWrite()

	if err != nil {
		return 0, nil, nil, err
	}

	return requestID, resp, writer, nil
}

// Close the connection.
func (sc *serveconn) Close() error {

	if limiter := sc.server.closeRateLimiter[sc.idx]; limiter != nil {
		limiter.Take()
	}

	ok, ch := sc.server.untrack(sc, false)
	if !ok {
		<-ch
	}
	return sc.closeUntracked()

}

func (sc *serveconn) closeUntracked() error {

	err := sc.rwc.Close()
	if err != nil {
		return err
	}
	sc.cancelCtx()

	ci := sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
	ci.l.Lock()
	ci.closed = true
	closeNotify := ci.closeNotify
	ci.closeNotify = nil
	respes := ci.respes
	ci.respes = nil
	ci.l.Unlock()

	for _, v := range respes {
		v.Close()
	}
	for _, f := range closeNotify {
		f()
	}

	return nil
}
