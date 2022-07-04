package qrpc

import (
	"net"
	"time"

	"crypto/tls"

	"github.com/go-kit/kit/metrics"
)

// CompressorCodec for compress
// **Important**: should try to do it in place if possible
type CompressorCodec interface {
	Encode([]byte) ([]byte, error)
	Decode([]byte) ([]byte, error)
}

// ServerLifecycleCallbacks for handling net.Con outside of handler
type ServerLifecycleCallbacks struct {
	// connection will be rejected if OnAccept returns non nil error
	OnAccept func(net.Conn) error
	// called after connection is closed
	OnClose func(net.Conn)
}

// ServerSubFunc for server subscribe callback
type ServerSubFunc func(*ConnectionInfo, *Frame)

// ServerBinding contains binding infos
type ServerBinding struct {
	Addr                            string
	Handler                         Handler // handler to invoke
	DefaultReadTimeout              int
	DefaultWriteTimeout             int
	WBufSize                        int // best effort only, check log for error
	RBufSize                        int // best effort only, check log for error
	ReadFrameChSize                 int
	WriteFrameChSize                int
	MaxFrameSize                    int
	MaxCloseRate                    int // per second
	MaxInboundFramePerSecond        int
	MaxInboundInflightStreamPerConn int32 // connection will be closed when exceeded
	ListenFunc                      func(network, address string) (net.Listener, error)
	Codec                           CompressorCodec
	OverlayNetwork                  func(net.Listener, *tls.Config) Listener
	SubFunc                         ServerSubFunc
	OnKickCB                        func(w FrameWriter)
	LatencyMetric                   metrics.Histogram
	CounterMetric                   metrics.Counter
	TLSConf                         *tls.Config
	LifecycleCallbacks              ServerLifecycleCallbacks
	ln                              Listener
}

// SubFunc for client subscribe callback
type SubFunc func(*Connection, *Frame)

// ClientLifecycleCallbacks for Connection
type ClientLifecycleCallbacks struct {
	OnConnect    func(*Connection)
	OnDisconnect func(*Connection)
}

// ConnectionConfig is conf for Connection
type ConnectionConfig struct {
	WriteTimeout       int
	ReadTimeout        int
	DialTimeout        time.Duration
	WriteFrameChSize   int
	WBufSize           int // best effort only, check log for error
	RBufSize           int // best effort only, check log for error
	Handler            Handler
	DialFunc           func(address string, dialConfig DialConfig) (net.Conn, error)
	Codec              CompressorCodec
	TLSConf            *tls.Config
	LifecycleCallbacks ClientLifecycleCallbacks
}

// DialConfig for dial
type DialConfig struct {
	DialTimeout time.Duration
	WBufSize    int // best effort only, check log for error
	RBufSize    int // best effort only, check log for error
	TLSConf     *tls.Config
}

// KeepAliveListenerConfig is config for KeepAliveListener
type KeepAliveListenerConfig struct {
	KeepAliveDuration time.Duration
	WriteBufferSize   int
	ReadBufferSize    int
}
