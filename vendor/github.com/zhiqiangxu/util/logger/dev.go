package logger

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	devLogger *zap.Logger
	devMU     sync.Mutex
)

// DevInstance returns the instance for develop environment
func DevInstance() *zap.Logger {
	if devLogger != nil {
		return devLogger
	}

	devMU.Lock()
	defer devMU.Unlock()
	if devLogger != nil {
		return devLogger
	}

	encoderConfig := zap.NewDevelopmentEncoderConfig()

	zconf := zap.Config{
		DisableCaller:     true,
		DisableStacktrace: true,
		Level:             zap.NewAtomicLevelAt(zapcore.DebugLevel),
		Development:       true,
		Encoding:          "json",
		EncoderConfig:     encoderConfig,
		OutputPaths:       []string{"stdout"},
		ErrorOutputPaths:  []string{"stderr"},
	}

	var err error
	devLogger, err = New(zconf)
	if err != nil {
		panic(fmt.Sprintf("DevInstance New:%v", err))
	}

	return devLogger
}
