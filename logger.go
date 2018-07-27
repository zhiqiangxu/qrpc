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

func logInfo(msg ...interface{}) {
	if Logger == nil {
		fmt.Fprint(os.Stdout, time.Now().String(), sep, fmt.Sprintln(msg...))
		return
	}
	Logger.Info(msg...)
}

func logError(msg ...interface{}) {
	if Logger == nil {
		fmt.Fprint(os.Stderr, time.Now().String(), sep, fmt.Sprintln(msg...))
		return
	}
	Logger.Error(msg...)
}

func logDebug(msg ...interface{}) {
	if Logger == nil {
		fmt.Fprint(os.Stdout, time.Now().String(), sep, fmt.Sprintln(msg...))
		return
	}
	Logger.Debug(msg...)
}
