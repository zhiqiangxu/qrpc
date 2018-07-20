package qrpc

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
)

// A serveconn represents the server side of an qrpc connection.
type serveconn struct {
	// server is the server on which the connection arrived.
	// Immutable; never nil.
	server *Server

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc
	// ctx is the corresponding context for cancelCtx
	ctx context.Context
	wg  sync.WaitGroup // wait group for goroutines

	idx int

	mu sync.Mutex
	id string // the only mutable

	closeCh chan struct{}

	// rwc is the underlying network connection.
	// This is never wrapped by other types and is the value given out
	// to CloseNotifier callers. It is usually of type *net.TCPConn
	rwc net.Conn

	reader       *defaultFrameReader    // used in conn.readFrames
	writer       FrameWriter            // used by handlers
	readFrameCh  chan readFrameResult   // written by conn.readFrames
	writeFrameCh chan writeFrameRequest // written by FrameWriter

}

// ConnectionInfoKey is context key for ConnectionInfo
// used to store custom information
var ConnectionInfoKey = &contextKey{"qrpc-connection"}

// ConnectionInfo for store info on connection
type ConnectionInfo struct {
	SC       *serveconn // read only
	anything interface{}
}

// Lock reuses the mu of serveconn
func (ci *ConnectionInfo) Lock() {
	ci.SC.mu.Lock()
}

// Unlock reuses the mu of serveconn
func (ci *ConnectionInfo) Unlock() {
	ci.SC.mu.Unlock()
}

// Store sets anything
func (ci *ConnectionInfo) Store(anything interface{}) {
	ci.SC.mu.Lock()
	ci.anything = anything
	ci.SC.mu.Unlock()
}

// StoreLocked sets anything without lock
func (ci *ConnectionInfo) StoreLocked(anything interface{}) {
	ci.anything = anything
}

// Load gets anything
func (ci *ConnectionInfo) Load(anything interface{}) interface{} {
	ci.SC.mu.Lock()
	defer ci.SC.mu.Unlock()
	return ci.anything
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

// Serve a new connection.
func (sc *serveconn) serve(ctx context.Context) {

	ctx, cancelCtx := context.WithCancel(ctx)
	ctx = context.WithValue(ctx, ConnectionInfoKey, &ConnectionInfo{SC: sc})

	sc.cancelCtx = cancelCtx
	sc.ctx = ctx

	idx := sc.idx

	defer func() {
		// connection level panic
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			sc.server.logf("qrpc: panic serving %v: %v\n%s", sc.rwc.RemoteAddr().String(), err, buf)
		}
		sc.Close()
	}()

	binding := sc.server.bindings[idx]
	sc.reader = newFrameReader(ctx, sc.rwc, binding.DefaultReadTimeout)
	sc.writer = newFrameWriter(ctx, sc.writeFrameCh) // only used by blocking mode

	goFunc(&sc.wg, func() {
		sc.readFrames()
	})
	goFunc(&sc.wg, func() {
		sc.writeFrames(binding.DefaultWriteTimeout)
	})

	handler := binding.Handler

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-sc.readFrameCh:

			if res.f.Flags&NBFlag == 0 {
				func() {
					sc.handleRequestPanic(res.f)
					handler.ServeQRPC(sc.writer, res.f)
				}()
				res.readMore()
			} else {
				res.readMore()
				goFunc(&sc.wg, func() {
					sc.handleRequestPanic(res.f)
					handler.ServeQRPC(sc.GetWriter(), res.f)
					sc.stopReadStream(res.f.RequestID)
				})
			}
		}

	}

}

func (sc *serveconn) stopReadStream(requestID uint64) {
	sc.reader.CloseStream(requestID)
}

func (sc *serveconn) handleRequestPanic(frame *RequestFrame) {
	if err := recover(); err != nil {
		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
		sc.server.logf("qrpc: handleRequestPanic %v: %v\n%s", sc.rwc.RemoteAddr().String(), err, buf)

		sc.stopReadStream(frame.RequestID)
		// send error frame
		writer := sc.GetWriter()
		writer.StartWrite(frame.RequestID, frame.Cmd, ErrorFlag)
		err = writer.EndWrite()
		if err != nil {
			sc.Close()
			return
		}

	}

}

// SetID sets id for serveconn
func (sc *serveconn) SetID(id string) {
	if id == "" {
		panic("empty id not allowed")
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.id = id
	sc.server.bindID(sc, id)
}

func (sc *serveconn) GetID() string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.id
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
	frame  []byte
	result chan error
}

// A gate lets two goroutines coordinate their activities.
type gate chan struct{}

func (g gate) Done() { g <- struct{}{} }

func (sc *serveconn) readFrames() (err error) {

	ctx := sc.ctx
	defer func() {
		if err != nil {
			sc.Close()
		}
	}()
	gate := make(gate)
	gateDone := gate.Done

	for {
		req, err := sc.reader.ReadFrame()
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
			_, err := writer.Write(res.frame)
			if err != nil {
				sc.Close()
			}
			res.result <- err
		case <-ctx.Done():
			return nil
		}
	}
}

// Close the connection.
func (sc *serveconn) Close() (<-chan struct{}, error) {

	return sc.closeLocked(false)

}

func (sc *serveconn) closeLocked(serverLocked bool) (<-chan struct{}, error) {
	err := sc.rwc.Close()
	if err != nil {
		return sc.closeCh, err
	}
	sc.cancelCtx()

	sc.server.untrack(sc, serverLocked)
	close(sc.closeCh)

	return sc.closeCh, nil
}
