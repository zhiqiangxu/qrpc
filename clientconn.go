package qrpc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"net"
	"sync"
	"time"
)

// Connection defines a qrpc connection
// it is thread safe
type Connection struct {
	// immutable
	net.Conn
	reader     *defaultFrameReader
	p          *sync.Pool
	conf       ConnectionConfig
	subscriber SubFunc // there can be only one subscriber because of streamed frames

	writeFrameCh chan writeFrameRequest

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc
	// ctx is the corresponding context for cancelCtx
	ctx context.Context
	wg  sync.WaitGroup // wait group for goroutines

	mu     sync.Mutex
	respes map[uint64]*response
}

// Response for response frames
type Response interface {
	GetFrame() *Frame
	GetFrameWithTimeout(time.Duration) *Frame
}

type response struct {
	Frame chan *Frame
}

func (r *response) GetFrame() *Frame {
	frame := <-r.Frame
	return frame
}

func (r *response) GetFrameWithTimeout(timeout time.Duration) *Frame {
	select {
	case frame := <-r.Frame:
		return frame
	case <-time.After(timeout):
		return nil
	}
}

func (r *response) SetResponse(frame *Frame) {
	r.Frame <- frame
}

func (r *response) Close() {
	close(r.Frame)
}

// NewConnection creates a connection without Client
func NewConnection(addr string, conf ConnectionConfig, f func(*Connection, *Frame)) (*Connection, error) {
	return newConnectionWithPool(addr, conf, nil, SubFunc(f))
}

func newConnectionWithPool(addr string, conf ConnectionConfig, p *sync.Pool, f SubFunc) (*Connection, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	if conf.Ctx == nil {
		conf.Ctx = context.Background()
	}

	c := &Connection{
		Conn: conn, conf: conf, subscriber: f, p: p,
		writeFrameCh: make(chan writeFrameRequest), respes: make(map[uint64]*response)}

	if p == nil {
		c.wakeup()
	}

	return c, nil
}

// called internally when using pool
func (conn *Connection) wakeup() {
	conn.ctx, conn.cancelCtx = context.WithCancel(conn.conf.Ctx)
	conn.reader = newFrameReader(conn.ctx, conn.Conn, conn.conf.ReadTimeout)

	GoFunc(&conn.wg, func() {
		conn.readFrames()
	})

	GoFunc(&conn.wg, func() {
		conn.writeFrames()
	})
}

// called internally when using pool
func (conn *Connection) suspend() {

	conn.cancelCtx()
	conn.wg.Wait()
}

// Wait until closed by peer
func (conn *Connection) Wait() {
	conn.wg.Wait()
}

// GetWriter return a FrameWriter
func (conn *Connection) GetWriter() FrameWriter {
	return newFrameWriter(conn.ctx, conn.writeFrameCh)
}

// StreamWriter is returned by StreamRequest
type StreamWriter interface {
	StartWrite(cmd Cmd)
	WriteBytes(v []byte)     // v is copied in WriteBytes
	EndWrite(end bool) error // block until scheduled
}

type defaultStreamWriter struct {
	w         *defaultFrameWriter
	requestID uint64
	flags     PacketFlag
}

// NewStreamWriter creates a streamwriter from StreamWriter
func NewStreamWriter(w FrameWriter, requestID uint64, flags PacketFlag) StreamWriter {
	dfr, ok := w.(*defaultFrameWriter)
	if !ok {
		return nil
	}
	return newStreamWriter(dfr, requestID, flags)
}

func newStreamWriter(w *defaultFrameWriter, requestID uint64, flags PacketFlag) StreamWriter {
	return &defaultStreamWriter{w: w, requestID: requestID, flags: flags}
}

func (dsw *defaultStreamWriter) StartWrite(cmd Cmd) {
	dsw.w.StartWrite(dsw.requestID, cmd, dsw.flags)
}

func (dsw *defaultStreamWriter) WriteBytes(v []byte) {
	dsw.w.WriteBytes(v)
}

func (dsw *defaultStreamWriter) EndWrite(end bool) error {
	return dsw.w.StreamEndWrite(end)
}

// StreamRequest is for streamed request
func (conn *Connection) StreamRequest(cmd Cmd, flags PacketFlag, payload []byte) (Response, StreamWriter, error) {

	flags |= StreamFlag | NBFlag
	requestID, resp, writer, err := conn.writeFirstFrame(cmd, flags, payload)
	if err != nil {
		return nil, nil, err
	}
	return resp, newStreamWriter(writer, requestID, flags), nil
}

// Request send a request frame and returns response frame
// error is non nil when write failed
func (conn *Connection) Request(cmd Cmd, flags PacketFlag, payload []byte) (Response, error) {

	_, resp, _, err := conn.writeFirstFrame(cmd, flags, payload)

	return resp, err
}

var (
	// ErrNoNewUUID when no new uuid available
	ErrNoNewUUID = errors.New("no new uuid available temporary")
)

func (conn *Connection) writeFirstFrame(cmd Cmd, flags PacketFlag, payload []byte) (uint64, Response, *defaultFrameWriter, error) {
	var (
		requestID uint64
		suc       bool
	)

	requestID = poorManUUID()
	conn.mu.Lock()
	i := 0
	for {
		_, ok := conn.respes[requestID]
		if !ok {
			suc = true
			break
		}

		i++
		if i >= 3 {
			break
		}
		requestID = poorManUUID()
	}

	if !suc {
		conn.mu.Unlock()
		return 0, nil, nil, ErrNoNewUUID
	}
	resp := &response{Frame: make(chan *Frame)}
	conn.respes[requestID] = resp
	conn.mu.Unlock()

	writer := newFrameWriter(conn.ctx, conn.writeFrameCh)
	writer.StartWrite(requestID, cmd, flags)
	writer.WriteBytes(payload)
	err := writer.EndWrite()

	if err != nil {
		conn.mu.Lock()
		delete(conn.respes, requestID)
		conn.mu.Unlock()
		return 0, nil, nil, err
	}

	return requestID, resp, writer, nil
}

// poorManUUID generate a uint64 uuid
func poorManUUID() (result uint64) {
	buf := make([]byte, 8)
	rand.Read(buf)
	result = binary.LittleEndian.Uint64(buf)
	if result == 0 {
		result = math.MaxUint64
	}
	return
}

// ErrConnAlreadyClosed when try to close an already closed conn
var ErrConnAlreadyClosed = errors.New("close an already closed conn")

// Close internally returns the connection to pool if not fatal
func (conn *Connection) Close(err error) error {

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.subscriber == nil {
		return ErrConnAlreadyClosed
	}

	conn.subscriber = nil
	for _, v := range conn.respes {
		v.Close()
	}

	conn.cancelCtx()

	var fatal bool
	if !(err == context.Canceled || err == context.DeadlineExceeded) {
		fatal = true
	}
	conn.respes = make(map[uint64]*response)
	if conn.p != nil && !fatal {
		conn.suspend()
		conn.p.Put(conn)
		return nil
	}

	return conn.Conn.Close()
}

var requestID uint64

func (conn *Connection) readFrames() {
	var (
		err   error
		frame *Frame
	)
	defer func() {
		conn.Close(err)
	}()
	for {
		frame, err = conn.reader.ReadFrame()
		if err != nil {
			return
		}

		if frame.Flags&PushFlag != 0 {
			// pushed frame
			if conn.subscriber != nil {
				conn.subscriber(conn, frame)
			}

			continue
		}

		// deal with pulled frames
		conn.mu.Lock()
		resp, ok := conn.respes[frame.RequestID]
		if !ok {
			//log error
			conn.mu.Unlock()
			continue
		}
		delete(conn.respes, frame.RequestID)
		conn.mu.Unlock()

		resp.SetResponse(frame)

	}
}

func (conn *Connection) writeFrames() (err error) {

	defer func() {
		conn.Close(err)
	}()
	writer := NewWriterWithTimeout(conn.ctx, conn.Conn, conn.conf.WriteTimeout)
	for {
		select {
		case res := <-conn.writeFrameCh:
			_, err := writer.Write(res.frame)
			res.result <- err
			if err != nil {
				return err
			}
		case <-conn.ctx.Done():
			return conn.ctx.Err()
		}
	}
}
