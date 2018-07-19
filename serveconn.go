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

// GetServer returns the server
func (sc *serveconn) GetServer() *Server {
	return sc.server
}

// Serve a new connection.
func (sc *serveconn) serve(ctx context.Context) {

	ctx, cancelCtx := context.WithCancel(ctx)
	sc.cancelCtx = cancelCtx
	sc.ctx = ctx

	idx := sc.idx

	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			sc.server.logf("http: panic serving %v: %v\n%s", sc.rwc.RemoteAddr().String(), err, buf)
		}
		sc.Close()
		cancelCtx()
	}()

	binding := sc.server.bindings[idx]
	sc.reader = newFrameReader(ctx, sc.rwc, binding.DefaultReadTimeout)
	sc.writer = newFrameWriter(ctx, sc.writeFrameCh) // only used by blocking mode

	go sc.readFrames()
	go sc.writeFrames(binding.DefaultWriteTimeout)

	handler := binding.Handler

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-sc.readFrameCh:
			if res.f.ctx != nil {
				panic("res.f.ctx is not nil")
			}
			res.f.ctx = ctx
			if res.f.Flags&NBFlag == 0 {
				handler.ServeQRPC(sc.writer, res.f)
				res.readMore()
			} else {
				res.readMore()
				go handler.ServeQRPC(sc.GetWriter(), res.f)
			}
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

func (sc *serveconn) Lock() {
	sc.mu.Lock()
}

func (sc *serveconn) Unlock() {
	sc.mu.Unlock()
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
	f *Frame // valid until readMore is called

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

func (c *serveconn) readFrames() (err error) {

	ctx := c.ctx
	defer func() {
		if err != nil {
			c.Close()
		}
	}()
	gate := make(gate)
	gateDone := gate.Done

	for {
		req, _, err := c.reader.ReadFrame()
		if err != nil {
			return err
		}
		select {
		case c.readFrameCh <- readFrameResult{f: req, readMore: gateDone}:
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
	writer := NewWriterWithTimeout(sc.rwc, timeout)
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

	err := sc.rwc.Close()
	if err != nil {
		return sc.closeCh, err
	}
	sc.cancelCtx()

	sc.server.untrack(sc)
	close(sc.closeCh)

	return sc.closeCh, nil
}
