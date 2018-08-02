package qrpc

import (
	"context"
	"sync"
)

// connstreams hosts all streams on connection
type connstreams struct {
	mu          sync.Mutex
	streams     map[uint64]*stream // non pushed streams
	pushstreams map[uint64]*stream // mutated 1. by framereader 2. G watching connection close

	wg sync.WaitGroup
}

func newConnStreams() *connstreams {
	return &connstreams{streams: make(map[uint64]*stream), pushstreams: make(map[uint64]*stream)}
}

func (cs *connstreams) Wait() {
	cs.wg.Wait()
	if len(cs.pushstreams) > 0 {
		panic("pushstreams not released")
	}
	if len(cs.streams) > 0 {
		panic("streams not released")
	}
}

// GetStream tries to get the associated stream, called by framereader for rst frame
func (cs *connstreams) GetStream(requestID uint64, flags FrameFlag) *stream {
	var target map[uint64]*stream
	if flags.IsPush() {
		target = cs.pushstreams
	} else {
		target = cs.streams
	}

	cs.mu.Lock()
	s, ok := target[requestID]
	cs.mu.Unlock()
	if ok {
		return s
	}

	return nil
}

// get or create the associated stream atomically
// if PushFlag is set, should only call CreateOrGetStream if caller is framereader
func (cs *connstreams) CreateOrGetStream(ctx context.Context, requestID uint64, flags FrameFlag) *stream {
	var (
		target map[uint64]*stream
	)
	if flags.IsPush() {
		target = cs.pushstreams
	} else {
		target = cs.streams
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	s, ok := target[requestID]
	if !ok {
		s = newStream(ctx, requestID, &cs.wg)
		target[requestID] = s
		s.NotifyWhenClose(func() {
			cs.mu.Lock()
			os, ok := target[requestID]
			if ok && os == s {
				delete(target, requestID)
			}
			cs.mu.Unlock()
			if !ok {
				panic("DeleteStream fail")
			}
		})
	}
	return s
}

type stream struct {
	ID         uint64
	frameCh    chan *Frame // always not nil
	ctx        context.Context
	cancelFunc context.CancelFunc

	mu          sync.Mutex
	closedSelf  bool
	closedPeer  bool
	fullClosed  bool
	binded      bool
	fullCloseCh chan struct{}
	closeNotify []func() // called when stream is fully closed
}

func newStream(ctx context.Context, requestID uint64, wg *sync.WaitGroup) *stream {
	ctx, cancelFunc := context.WithCancel(ctx)
	s := &stream{ctx: ctx, cancelFunc: cancelFunc, ID: requestID, frameCh: make(chan *Frame), fullCloseCh: make(chan struct{})}

	GoFunc(wg, func() {
		select {
		case <-ctx.Done():
			s.ensureClose()
		}
	})
	return s
}

func (s *stream) NotifyWhenClose(f func()) {
	s.mu.Lock()
	s.closeNotify = append(s.closeNotify, f)
	s.mu.Unlock()
}

func (s *stream) IsSelfClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closedSelf
}

// returns true if the stream has not been binded to any frame yet
// streamreader will first call TryBind, and if fail, call AddInFrame
func (s *stream) TryBind(firstFrame *Frame) bool {

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.binded {
		return false
	}

	s.closePeerIfNeeded(firstFrame.Flags)

	firstFrame.Stream = s
	firstFrame.ctx = s.ctx // TODO remove ctx assignment in framereader

	s.binded = true
	return true
}

// Done returns a channel for caller to wait for stream close
// when channel returned, stream is cleanly closed
func (s *stream) Done() <-chan struct{} {
	return s.fullCloseCh
}

// AddInFrame tries to add an inframe to stream
// framereader will call AddInFrame after TryBind fails
// return value means whether accepted by stream
// if not accepted, framereader should wait until stream closed,
// and call DeleteStream then CreateOrGetStream again
func (s *stream) AddInFrame(frame *Frame) bool {
	s.mu.Lock()
	if s.closedPeer {
		s.mu.Unlock()
		return false
	}

	s.mu.Unlock()

	select {
	case s.frameCh <- frame:
		s.mu.Lock()
		s.closePeerIfNeeded(frame.Flags)
		s.mu.Unlock()
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *stream) closePeerIfNeeded(flags FrameFlag) {
	if flags.IsPush() {
		s.closedSelf = true
	}
	if s.closedPeer {
		return
	}

	if flags.IsDone() {
		s.closedPeer = true
		close(s.frameCh)
		if s.closedSelf {
			s.reset()
		}
	}
}

// AddInFrame tries to add an nonpushed outframe to stream
// framewriter will blindly call AddOutFrame
// return value means whether accepted by stream
// if not accepted, framewriter should throw away the frame
// if accepted, framewriter can go ahead actually sending the frame
// for rst frame, return false mean there's no need to send rst frame
func (s *stream) AddOutFrame(requestID uint64, flags FrameFlag) bool {

	isRst := flags.IsRst()

	s.mu.Lock()
	defer s.mu.Unlock()

	if isRst {
		s.closedSelf = true
		if s.closedPeer {
			s.reset()
			return false
		}
		return true
	}

	if s.closedSelf {

		return false
	}

	if flags.IsDone() {
		s.closedSelf = true
		if s.closedPeer {
			s.reset()
		}
	}

	return true
}

func (s *stream) ResetByPeer() {
	s.mu.Lock()
	if !s.closedPeer {
		s.closedPeer = true
		close(s.frameCh)
	}
	s.mu.Unlock()

	s.reset()
}

func (s *stream) ensureClose() {
	s.mu.Lock()
	if !s.closedPeer {
		s.closedPeer = true
		close(s.frameCh)
	}
	closeNotify := s.closeNotify
	s.closeNotify = nil
	fullClosed := s.fullClosed
	if !fullClosed {
		s.fullClosed = true
	}
	s.mu.Unlock()
	for _, f := range closeNotify {
		f()
	}
	closeNotify = nil
	if !fullClosed {
		close(s.fullCloseCh)
	}
}

func (s *stream) reset() {

	s.cancelFunc()

}
