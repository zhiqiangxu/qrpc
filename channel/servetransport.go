package channel

import (
	"context"

	"github.com/zhiqiangxu/qrpc"
)

type (
	// Handler for ServeQRPC like channels
	Handler interface {
		ServeSRT(Sender, Receiver, Transport)
	}
	// HandlerFunc for anonymous usage
	HandlerFunc func(Sender, Receiver, Transport)

	serveSender struct {
		w     qrpc.FrameWriter
		frame *qrpc.RequestFrame
	}
	serveReceiver struct {
		frame                 *qrpc.RequestFrame
		hasReceivedFirstFrame bool
	}
	// ServeHandler is a qrpc.Handler
	ServeHandler struct {
		handler Handler
	}
)

// ServeSRT implements channel.Handler for HandlerFunc.
func (f HandlerFunc) ServeSRT(s Sender, r Receiver, t Transport) {
	f(s, r, t)
}

// NewQRPCHandler is ctor for ServeHandler
func NewQRPCHandler(handler Handler) qrpc.Handler {
	return &ServeHandler{handler: handler}
}

// ServeQRPC implements qrpc.Handler
func (h *ServeHandler) ServeQRPC(w qrpc.FrameWriter, frame *qrpc.RequestFrame) {
	sender := &serveSender{w: w, frame: frame}
	receiver := &serveReceiver{frame: frame}
	h.handler.ServeSRT(sender, receiver, NewTransport(frame.StreamInitiator()))
}

func (s *serveSender) Send(ctx context.Context, cmd qrpc.Cmd, msg interface{}, end bool) (err error) {
	bytes, err := Marshal(msg)
	if err != nil {
		return
	}

	flag := qrpc.StreamFlag
	if end {
		flag |= qrpc.StreamEndFlag
	}

	s.w.StartWrite(s.frame.RequestID, cmd, flag)
	s.w.WriteBytes(bytes)
	err = s.w.EndWrite()
	return
}

func (s *serveSender) End() (err error) {
	err = s.w.ResetFrame(s.frame.RequestID, 0)
	return
}

func (r *serveReceiver) Receive(ctx context.Context, cmd *qrpc.Cmd, msg interface{}) (err error) {
	frame := (*qrpc.Frame)(r.frame)

	if r.hasReceivedFirstFrame {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case frame = <-r.frame.FrameCh():
			if frame == nil {
				err = ErrStreamFinished
				return
			}
		}
	} else {
		r.hasReceivedFirstFrame = true
	}

	if cmd != nil {
		*cmd = frame.Cmd
		err = Unmarshal(frame.Payload, msg)
	}
	return
}
