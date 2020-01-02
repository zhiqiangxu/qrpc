package logger

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	prodLogger *zap.Logger
	prodMU     sync.Mutex
)

// ProdInstance returns the instance for production environment
func ProdInstance() *zap.Logger {
	if prodLogger != nil {
		return prodLogger
	}

	prodMU.Lock()
	defer prodMU.Unlock()
	if prodLogger != nil {
		return prodLogger
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder

	zconf := zap.Config{
		DisableCaller:     true,
		DisableStacktrace: true,
		Level:             zap.NewAtomicLevelAt(zapcore.InfoLevel),
		Development:       false,
		Encoding:          "json",
		EncoderConfig:     encoderConfig,
		OutputPaths:       []string{"stdout"},
		ErrorOutputPaths:  []string{"stderr"},
	}

	var err error
	prodLogger, err = New(zconf)
	if err != nil {
		panic(fmt.Sprintf("ProdInstance New:%v", err))
	}

	return prodLogger
}
