package qrpc

import (
	"context"
	"errors"
	"net"
	"runtime"
)

// A conn represents the server side of an qrpc connection.
type conn struct {
	// server is the server on which the connection arrived.
	// Immutable; never nil.
	server *Server

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc

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

// Serve a new connection.
func (c *conn) serve(ctx context.Context, idx int) {

	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			c.server.logf("http: panic serving %v: %v\n%s", c.rwc.RemoteAddr().String(), err, buf)
		}
		c.close()
	}()

	ctx, cancelCtx := context.WithCancel(ctx)
	c.cancelCtx = cancelCtx
	defer cancelCtx()

	c.reader = newFrameReader(ctx, c.rwc, c.server.bindings[idx].DefaultReadTimeout)
	c.writer = NewFrameWriter(ctx, c.writeFrameCh)

	go c.readFrames(ctx)
	go c.writeFrames(ctx, c.server.bindings[idx].DefaultWriteTimeout)

	handler := c.server.bindings[idx].Handler

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-c.readFrameCh:
			if res.f.ctx != nil {
				panic("res.f.ctx is not nil")
			}
			res.f.ctx = ctx
			if res.f.Flags&NBFlag == 0 {
				handler.ServeQRPC(c.writer, res.f)
				res.readMore()
			} else {
				res.readMore()
				go handler.ServeQRPC(c.writer, res.f)
			}
		}

	}

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

func (c *conn) readFrames(ctx context.Context) (err error) {

	defer func() {
		if err != nil {
			c.close()
		}
	}()
	gate := make(gate)
	gateDone := gate.Done

	for {
		req, err := c.reader.ReadFrame()
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

func (c *conn) writeFrames(ctx context.Context, timeout int) (err error) {

	writer := NewWriterWithTimeout(c.rwc, timeout)
	for {
		select {
		case res := <-c.writeFrameCh:
			_, err := writer.Write(res.frame)
			if err != nil {
				c.close()
			}
			res.result <- err
		case <-ctx.Done():
			return nil
		}
	}
}

// Close the connection.
func (c *conn) close() {
	c.finalFlush()
	c.rwc.Close()
	c.cancelCtx()
}

// return to pool
func (c *conn) finalFlush() {
}
