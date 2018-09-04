package qrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync"
	"time"
	"unsafe"
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

	cs *connstreams

	// rwc is the underlying network connection.
	// This is never wrapped by other types and is the value given out
	// to CloseNotifier callers. It is usually of type *net.TCPConn
	rwc net.Conn

	reader       *defaultFrameReader    // used in conn.readFrames
	writer       FrameWriter            // used by handlers
	readFrameCh  chan readFrameResult   // written by conn.readFrames
	writeFrameCh chan writeFrameRequest // written by FrameWriter

	// modified by Server
	untrack     uint32 // ony the first call to untrack actually do it, subsequent calls should wait for untrackedCh
	untrackedCh chan struct{}
}

// ConnectionInfoKey is context key for ConnectionInfo
// used to store custom information
var ConnectionInfoKey = &contextKey{"qrpc-connection"}

// ConnectionInfo for store info on connection
type ConnectionInfo struct {
	l           sync.Mutex
	closed      bool
	id          string
	closeNotify []func()
	SC          *serveconn
	anything    interface{}
}

// GetAnything returns anything
func (ci *ConnectionInfo) GetAnything() interface{} {
	ci.l.Lock()
	defer ci.l.Unlock()
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
	ci.l.Lock()
	defer ci.l.Unlock()
	return ci.id
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

// ReaderConfig for change timeout
type ReaderConfig interface {
	SetReadTimeout(timeout int)
}

// Reader returns the ReaderConfig
func (sc *serveconn) Reader() ReaderConfig {
	return sc.reader
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
			logError("connection panic", sc.rwc.RemoteAddr().String(), err, buf)
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
	sc.reader = newFrameReaderWithMFS(ctx, sc.rwc, binding.DefaultReadTimeout, maxFrameSize)
	sc.writer = newFrameWriter(ctx, sc.writeFrameCh) // only used by blocking mode

	GoFunc(&sc.wg, func() {
		sc.readFrames()
	})
	GoFunc(&sc.wg, func() {
		sc.writeFrames(binding.DefaultWriteTimeout)
	})

	handler := binding.Handler

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-sc.readFrameCh:

			if !res.f.Flags.IsNonBlock() {
				func() {
					defer sc.handleRequestPanic(res.f, time.Now())
					handler.ServeQRPC(sc.writer, res.f)
				}()
				res.readMore()
			} else {
				res.readMore()
				GoFunc(&sc.wg, func() {
					defer sc.handleRequestPanic(res.f, time.Now())
					handler.ServeQRPC(sc.GetWriter(), res.f)
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
		logError("handleRequestPanic", sc.rwc.RemoteAddr().String(), err, string(buf))

	}

	s := frame.Stream
	if !s.IsSelfClosed() {
		// send error frame
		writer := sc.GetWriter()
		writer.StartWrite(frame.RequestID, 0, StreamRstFlag)
		err := writer.EndWrite()
		if err != nil {
			logDebug("send error frame", err, sc.rwc.RemoteAddr().String(), frame)
		}
	}

}

// SetID sets id for serveconn
func (sc *serveconn) SetID(id string) {
	if id == "" {
		panic("empty id not allowed")
	}
	ci := sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
	ci.l.Lock()
	if ci.id != "" {
		ci.l.Unlock()
		panic(fmt.Sprintf("SetID called twice: %s vs %s", id, ci.id))
	}
	ci.id = id
	ci.l.Unlock()

	sc.server.bindID(sc, id)
}

func (sc *serveconn) GetID() string {
	ci := sc.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
	return ci.GetID()
}

// GetWriter generate a FrameWriter for the connection
func (sc *serveconn) GetWriter() FrameWriter {

	return newFrameWriter(sc.ctx, sc.writeFrameCh)
}

// ErrInvalidPacket when packet invalid
var ErrInvalidPacket = errors.New("invalid packet")

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

func (g gate) Done() { g <- struct{}{} }

func (sc *serveconn) readFrames() (err error) {

	ctx := sc.ctx
	defer func() {
		logDebug(unsafe.Pointer(sc), "readFrames", err)
		if err != nil {
			sc.Close()
		}
	}()
	gate := make(gate, 1)
	gateDone := gate.Done

	for {
		req, err := sc.reader.ReadFrame(sc.cs)
		if err != nil {
			return err
		}
		select {
		case sc.readFrameCh <- readFrameResult{f: (*RequestFrame)(req), readMore: gateDone}:
		case <-ctx.Done():
			return nil
		}

		select {
		case <-gate:
		case <-ctx.Done():
			return nil
		}
	}

}

func (sc *serveconn) writeFrames(timeout int) (err error) {

	ctx := sc.ctx
	writer := NewWriterWithTimeout(ctx, sc.rwc, timeout)
	for {
		select {
		case res := <-sc.writeFrameCh:
			dfw := res.dfw
			flags := dfw.Flags()
			requestID := dfw.RequestID()

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
				s := sc.cs.CreateOrGetStream(sc.ctx, requestID, flags)
				if !s.AddOutFrame(requestID, flags) {
					res.result <- ErrWriteAfterCloseSelf
					break
				}
			}

			_, err := writer.Write(dfw.GetWbuf())
			if err != nil {
				logDebug(unsafe.Pointer(sc), "serveconn Write", err)
				sc.Close()
			}
			res.result <- err
		case <-ctx.Done():
			return nil
		}
	}
}

// Close the connection.
func (sc *serveconn) Close() error {

	logDebug(unsafe.Pointer(sc), "Close")
	ok, ch := sc.server.untrack(sc)
	if !ok {
		<-ch
	}
	return sc.closeUntracked()

}

func (sc *serveconn) closeUntracked() error {

	logDebug(unsafe.Pointer(sc), "closeUntracked")
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
	ci.l.Unlock()

	for _, f := range closeNotify {
		f()
	}
	return nil
}
