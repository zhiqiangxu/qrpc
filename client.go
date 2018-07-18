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
// it is not thread safe, because writer,resp is not
type Connection struct {
	net.Conn
	reader     *defaultFrameReader
	writer     *Writer
	p          *sync.Pool
	conf       ConnectionConfig
	subscriber SubFunc // there can be only one subscriber because of streamed frames
	mu         sync.Mutex
	respes     sync.Map // map[uint64]*response
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

	c := &Connection{Conn: conn, conf: conf, subscriber: f}
	c.reader = newFrameReader(conf.Ctx, conn, conf.ReadTimeout)
	c.writer = NewWriterWithTimeout(conn, conf.WriteTimeout)

	go c.readFrames()

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

var (
	// ErrNoNewUUID when no new uuid available
	ErrNoNewUUID = errors.New("no new uuid available temporary")
)

// Request send a request frame and returns response frame
// error is non nil when write failed
func (conn *Connection) Request(cmd Cmd, flags PacketFlag, payload []byte) (Response, error) {

	var (
		requestID uint64
		suc       bool
	)
	for i := 0; i < 3; i++ {
		requestID = PoorManUUID()
		_, ok := conn.respes.Load(requestID)
		if !ok {
			suc = true
			break
		}
	}
	if !suc {
		return nil, ErrNoNewUUID
	}

	resp := &response{Frame: make(chan *Frame)}
	conn.respes.Store(requestID, resp)

	packet := MakePacket(requestID, cmd, flags, payload)
	_, err := conn.writer.Write(packet)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// MakePacket generate packet
func MakePacket(requestID uint64, cmd Cmd, flags PacketFlag, payload []byte) []byte {
	length := 12 + uint32(len(payload))
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf, length)
	binary.BigEndian.PutUint64(buf[4:], requestID)
	cmdAndFlags := uint32(cmd&0xffffff) + uint32(flags)<<24
	binary.BigEndian.PutUint32(buf[12:], uint32(cmdAndFlags))
	copy(buf[16:], payload)
	return buf
}

// PoorManUUID generate a uint64 uuid
func PoorManUUID() (result uint64) {
	buf := make([]byte, 8)
	rand.Read(buf)
	result = binary.LittleEndian.Uint64(buf)
	if result == 0 {
		result = math.MaxUint64
	}
	return
}

// Close internally returns the connection to pool
func (conn *Connection) Close() error {
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
		resp, ok := conn.respes.Load(frame.RequestID)
		if !ok {
			//log error
			continue
		}
		resp.(*response).SetResponse(frame)
		conn.respes.Delete(frame.RequestID)

	}
}

// Subscribe register f as callback for pushed message
func (conn *Connection) Subscribe(f func(*Frame)) {
	if conn.subscriber != nil {
		panic("only one subscriber allowed")
	}
	conn.subscriber = f
}
