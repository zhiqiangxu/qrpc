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

	id string // the only mutable

	untrack     uint32 // ony the first call to untrack actually do it, subsequent calls should wait for untrackedCh
	untrackedCh chan struct{}

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
	Anything interface{}
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
	sc.id = id
	sc.server.bindID(sc, id)
}

func (sc *serveconn) GetID() string {
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
func (sc *serveconn) Close() error {

	ok, ch := sc.server.untrack(sc)
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

	return nil
}
