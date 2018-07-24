package qrpc

import (
	"context"
)

// Frame models a qrpc frame
// all fields are readly only
type Frame struct {
	RequestID uint64
	Flags     PacketFlag
	Cmd       Cmd
	Payload   []byte
	frameCh   chan *Frame // non nil for the first frame in stream

	// ctx is either the client or server context. It should only
	// be modified via copying the whole Request using WithContext.
	// It is unexported to prevent people from using Context wrong
	// and mutating the contexts held by callers of the same request.
	ctx context.Context //fyi: https://www.reddit.com/r/golang/comments/69j71a/why_does_httprequestwithcontext_do_a_shallow_copy/
}

// FrameCh get the next frame ch
func (r *Frame) FrameCh() <-chan *Frame {
	return r.frameCh
}

// Context returns the request's context. To change the context, use
// WithContext.
//
// The returned context is always non-nil; it defaults to the
// background context.
//
// For outgoing client requests, the context controls cancelation.
//
// For incoming server requests, the context is canceled when the
// client's connection closes, the request is canceled (with HTTP/2),
// or when the ServeHTTP method returns.
func (r *Frame) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// WithContext returns a shallow copy of r with its context changed
// to ctx. The provided ctx must be non-nil.
func (r *Frame) WithContext(ctx context.Context) *Frame {
	if ctx == nil {
		panic("nil context")
	}
	r2 := new(Frame)
	*r2 = *r
	r2.ctx = ctx

	return r2
}

// RequestFrame is client->server
type RequestFrame Frame

// ConnectionInfo returns the underlying ConnectionInfo
func (r *RequestFrame) ConnectionInfo() *ConnectionInfo {

	return r.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)

}

// Close the underlying connection
func (r *RequestFrame) Close() error {

	ci := r.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
	ci.SC.Close()
	return nil
}
