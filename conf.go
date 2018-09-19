package qrpc

import (
	"time"

	"github.com/go-kit/kit/metrics"
)

// ServerBinding contains binding infos
type ServerBinding struct {
	Addr                string
	Handler             Handler // handler to invoke
	DefaultReadTimeout  int
	DefaultWriteTimeout int
	MaxFrameSize        int
	LatencyMetric       metrics.Histogram
	CounterMetric       metrics.Counter
}

// SubFunc for subscribe callback
type SubFunc func(*Connection, *Frame)

// ConnectionConfig is conf for Connection
type ConnectionConfig struct {
	WriteTimeout int
	ReadTimeout  int
	DialTimeout  time.Duration
}
