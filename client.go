package qrpc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"net"
	"sync"
)

// Client defines a qrpc client
type Client struct {
	mu          sync.Mutex
	conf        ConnectionConfig
	connections map[string]*sync.Pool
}

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

	mu     sync.Mutex
	respes map[uint64]*response
}

// NewClient creates a Client instance
func NewClient(conf ConnectionConfig) *Client {
	cli := &Client{conf: conf, connections: make(map[string]*sync.Pool)}
	return cli
}

// NewConnection creates a connection without Client
func NewConnection(addr string, conf ConnectionConfig, f func(*Frame)) (*Connection, error) {
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
		Conn: conn, conf: conf, subscriber: f,
		writeFrameCh: make(chan writeFrameRequest), respes: make(map[uint64]*response)}
	c.reader = newFrameReader(conf.Ctx, conn, conf.ReadTimeout)

	go c.readFrames()
	go c.writeFrames()

	return c, nil
}

// GetConn get a connection from Client
func (cli *Client) GetConn(addr string, f func(*Frame)) *Connection {

	cli.mu.Lock()

	p, ok := cli.connections[addr]
	if !ok {
		p = &sync.Pool{}
		newFunc := func() interface{} {

			conn, err := newConnectionWithPool(addr, cli.conf, p, SubFunc(f))
			if err != nil {
				return nil
			}

			return conn
		}
		p.New = newFunc
		cli.connections[addr] = p
	}
	cli.mu.Unlock()

	conn, ok := p.Get().(*Connection)
	if !ok {
		return nil
	}
	conn.subscriber = f
	return conn
}

// Response for response frames
type Response interface {
	GetFrame() *Frame
}

type response struct {
	Frame chan *Frame
}

func (r *response) GetFrame() *Frame {
	frame := <-r.Frame
	return frame
}

func (r *response) SetResponse(frame *Frame) {
	r.Frame <- frame
}

func (r *response) Close() {
	close(r.Frame)
}

var (
	// ErrNoNewUUID when no new uuid available
	ErrNoNewUUID = errors.New("no new uuid available temporary")
)

// GetWriter return a FrameWriter
func (conn *Connection) GetWriter() FrameWriter {
	return newFrameWriter(conn.conf.Ctx, conn.writeFrameCh)
}

// StreamWriter is returned by StreamRequest
type StreamWriter interface {
	StartWrite()
	WriteBytes(v []byte)     // v is copied in WriteBytes
	EndWrite(end bool) error // block until scheduled
}

type defaultStreamWriter struct {
	w         *defaultFrameWriter
	requestID uint64
	cmd       Cmd
	flags     PacketFlag
}

func newStreamWriter(w *defaultFrameWriter, requestID uint64, cmd Cmd, flags PacketFlag) *defaultStreamWriter {
	return &defaultStreamWriter{w: w, requestID: requestID, cmd: cmd, flags: flags}
}

func (dsw *defaultStreamWriter) StartWrite() {
	dsw.w.StartWrite(dsw.requestID, dsw.cmd, dsw.flags)
}

func (dsw *defaultStreamWriter) WriteBytes(v []byte) {
	dsw.w.WriteBytes(v)
}

func (dsw *defaultStreamWriter) EndWrite(end bool) error {
	return dsw.w.StreamEndWrite(end)
}

// StreamRequest is for streamed request
func (conn *Connection) StreamRequest(cmd Cmd, flags PacketFlag, payload []byte) (Response, StreamWriter, error) {

	requestID, resp, writer, err := conn.writeFirstFrame(cmd, flags, payload)
	if err != nil {
		return nil, nil, err
	}
	return resp, newStreamWriter(writer, requestID, cmd, flags), nil
}

// Request send a request frame and returns response frame
// error is non nil when write failed
func (conn *Connection) Request(cmd Cmd, flags PacketFlag, payload []byte) (Response, error) {

	_, resp, _, err := conn.writeFirstFrame(cmd, flags, payload)

	return resp, err
}

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

	writer := newFrameWriter(conn.conf.Ctx, conn.writeFrameCh)
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

// Close internally returns the connection to pool
func (conn *Connection) Close() error {

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.subscriber == nil {
		return ErrConnAlreadyClosed
	}

	conn.subscriber = nil
	for _, v := range conn.respes {
		v.Close()
	}

	conn.respes = make(map[uint64]*response)
	if conn.p != nil {
		conn.p.Put(conn)
		return nil
	}

	return conn.Conn.Close()
}

var requestID uint64

func (conn *Connection) readFrames() {
	defer func() {
		conn.Close()
	}()
	for {
		frame, err := conn.reader.ReadFrame()
		if err != nil {
			return
		}

		if frame.Flags&PushFlag != 0 {
			// pushed frame
			if conn.subscriber != nil {
				conn.subscriber(frame)
			}

			return
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
		if err != nil {
			conn.Close()
		}
	}()
	writer := NewWriterWithTimeout(conn.Conn, conn.conf.WriteTimeout)
	for {
		select {
		case res := <-conn.writeFrameCh:
			_, err = writer.Write(res.frame)
			res.result <- err
			if err != nil {
				return
			}
		case <-conn.conf.Ctx.Done():
			return nil
		}
	}
}
