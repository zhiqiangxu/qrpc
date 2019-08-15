package qrpc

import (
	"fmt"
	"os"
	"time"
)

type logger interface {
	Info(msg ...interface{})
	Error(msg ...interface{})
	Debug(msg ...interface{})
}

const (
	sep = " : "
)

// Logger is exported logger interface
var Logger logger

// LogInfo for info log
func LogInfo(msg ...interface{}) {
	if Logger == nil {
		fmt.Fprint(os.Stdout, time.Now().String(), sep, fmt.Sprintln(msg...))
		return
	}
	Logger.Info(msg...)
}

// LogError for error log
func LogError(msg ...interface{}) {
	if Logger == nil {
		fmt.Fprint(os.Stderr, time.Now().String(), sep, fmt.Sprintln(msg...))
		return
	}
	Logger.Error(msg...)
}

// LogDebug for debug log
func LogDebug(msg ...interface{}) {
	if Logger == nil {
		// fmt.Fprint(os.Stdout, time.Now().String(), sep, fmt.Sprintln(msg...))
		return
	}
	Logger.Debug(msg...)
}
