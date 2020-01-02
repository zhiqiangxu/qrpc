package logger

import (
	"fmt"

	"go.uber.org/zap"
)

// EnvType for environment type
type EnvType int

const (
	// Prod for production environment (default)
	Prod EnvType = iota
	// Dev for develop environment
	Dev
)

// Env for chosen environment
var Env EnvType

// Instance for chosen logger
func Instance() *zap.Logger {
	switch Env {
	case Prod:
		return ProdInstance()
	case Dev:
		return DevInstance()
	default:
		panic(fmt.Sprintf("unknown EnvType:%v", Env))
	}
}
