package channel

import (
	"context"
	"errors"
	"sync"

	"github.com/zhiqiangxu/qrpc"
)

type (
	clientTransport struct {
		c qrpc.StreamInitiator
	}

	clientSender struct {
		sync.RWMutex
		t            *clientTransport
		r            *clientReceiver
		streamWriter qrpc.StreamWriter
		resp         qrpc.Response
		firstFrame   *qrpc.Frame
		closed       bool
	}

	clientReceiver struct {
		s *clientSender
	}
)

// NewClientTransport creates a Transport for client
func NewClientTransport(c qrpc.StreamInitiator) Transport {
	return &clientTransport{c: c}
}

func (t *clientTransport) Pipe() (s Sender, r Receiver, err error) {
	if t.c.IsClosed() {
		err = ErrClientAlreadyClosed
		return
	}

	receiver := &clientReceiver{}
	sender := &clientSender{t: t, r: receiver}
	receiver.s = sender

	s = sender
	r = receiver
	return
}

var (
	// ErrClientAlreadyClosed when client already closed
	ErrClientAlreadyClosed = errors.New("client already closed")
	// ErrSenderClosed when sender closed
	ErrSenderClosed = errors.New("sender closed")
	// ErrSenderCloseTwice when Close a sender twice
	ErrSenderCloseTwice = errors.New("sender close twice")
	// ErrNoResp when no resp
	ErrNoResp = errors.New("no resp")
	// ErrStreamFinished when stream finished
	ErrStreamFinished = errors.New("stream finished")
)

func (s *clientSender) Send(ctx context.Context, cmd qrpc.Cmd, msg interface{}, end bool) (err error) {
	bytes, err := Marshal(msg)
	if err != nil {
		return
	}

	s.RLock()
	defer s.RUnlock()

	if s.closed {
		err = ErrSenderClosed
		return
	}

	var flag qrpc.FrameFlag
	if end {
		flag = qrpc.StreamEndFlag
	}
	if s.streamWriter == nil {
		s.streamWriter, s.resp, err = s.t.c.StreamRequest(cmd, flag, bytes)
		return
	}

	s.streamWriter.StartWrite(cmd)
	s.streamWriter.WriteBytes(bytes)
	err = s.streamWriter.EndWrite(false)

	return
}

func (s *clientSender) End() (err error) {
	s.Lock()
	defer s.Unlock()

	if s.closed {
		err = ErrSenderCloseTwice
		return
	}

	s.closed = true
	if s.streamWriter == nil {
		return
	}

	err = s.streamWriter.ResetFrame(0)
	return
}

func (s *clientSender) getFrame(ctx context.Context) (frame *qrpc.Frame, err error) {

	if s.resp == nil {
		err = ErrNoResp
		return
	}

	if s.firstFrame != nil {

		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case frame = <-s.firstFrame.FrameCh():
			if frame == nil {
				err = ErrStreamFinished
				return
			}
			return
		}
	}
	frame, err = s.resp.GetFrameWithContext(ctx)
	s.firstFrame = frame

	return
}

func (r *clientReceiver) Receive(ctx context.Context, cmd *qrpc.Cmd, msg interface{}) (err error) {
	frame, err := r.s.getFrame(ctx)
	if err != nil {
		return
	}
	if cmd != nil {
		*cmd = frame.Cmd
	}
	err = Unmarshal(frame.Payload, msg)
	return
}
