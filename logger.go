package qrpc

type logger interface {
	Info(msg ...interface{})
	Error(msg ...interface{})
	Debug(msg ...interface{})
}

// Logger is exported logger interface
var Logger logger
