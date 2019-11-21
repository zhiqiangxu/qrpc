package qrpc

import (
	"fmt"

	"github.com/zhiqiangxu/util/logger"
	"go.uber.org/zap"
)

var (
	// Logger if exported for overwrite
	l *zap.Logger
)

// Logger returns the logger for qrpc
func Logger() *zap.Logger {
	return l
}

// SetLogger for change zap.Logger
// should only be called in init func
func SetLogger(zl *zap.Logger) {
	l = zl
}

func init() {
	if l == nil {

		var err error
		config := zap.Config{
			DisableCaller:     true,
			DisableStacktrace: true,
			Level:             zap.NewAtomicLevelAt(zap.InfoLevel),
			Encoding:          "json",
			EncoderConfig:     zap.NewDevelopmentEncoderConfig(),
			OutputPaths:       []string{"stdout"},
			ErrorOutputPaths:  []string{"stderr"},
		}
		l, err = logger.New(config)
		if err != nil {
			panic(fmt.Sprintf("qrpc.zap.Build:%v", err))
		}
	}
}
