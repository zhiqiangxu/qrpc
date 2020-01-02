package logger

import (
	"errors"
	"sort"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var errEncodingNotSupported = errors.New("encoding not supported")

// New is similar to Config.Build except that info and error logs are separated
// only json/console encoder is supported (zap doesn't provide a way to refer to other encoders)
func New(cfg zap.Config) (logger *zap.Logger, err error) {

	sink, errSink, err := openSinks(cfg)
	if err != nil {
		return
	}

	var encoder zapcore.Encoder
	switch cfg.Encoding {
	case "json":
		encoder = zapcore.NewJSONEncoder(cfg.EncoderConfig)
	case "console":
		encoder = zapcore.NewConsoleEncoder(cfg.EncoderConfig)
	default:
		err = errEncodingNotSupported
		return
	}

	stdoutPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl == zapcore.InfoLevel
	})
	stderrPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.ErrorLevel
	})

	core := zapcore.NewTee(
		zapcore.NewCore(encoder, sink, stdoutPriority),
		zapcore.NewCore(encoder, errSink, stderrPriority),
	)

	return zap.New(core, buildOptions(cfg, errSink)...), nil
}

func openSinks(cfg zap.Config) (zapcore.WriteSyncer, zapcore.WriteSyncer, error) {
	sink, closeOut, err := zap.Open(cfg.OutputPaths...)
	if err != nil {
		return nil, nil, err
	}
	errSink, _, err := zap.Open(cfg.ErrorOutputPaths...)
	if err != nil {
		closeOut()
		return nil, nil, err
	}
	return sink, errSink, nil
}

func buildOptions(cfg zap.Config, errSink zapcore.WriteSyncer) []zap.Option {
	opts := []zap.Option{zap.ErrorOutput(errSink)}

	if cfg.Development {
		opts = append(opts, zap.Development())
	}

	if !cfg.DisableCaller {
		opts = append(opts, zap.AddCaller())
	}

	stackLevel := zap.ErrorLevel
	if cfg.Development {
		stackLevel = zap.WarnLevel
	}
	if !cfg.DisableStacktrace {
		opts = append(opts, zap.AddStacktrace(stackLevel))
	}

	if cfg.Sampling != nil {
		opts = append(opts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSampler(core, time.Second, int(cfg.Sampling.Initial), int(cfg.Sampling.Thereafter))
		}))
	}

	if len(cfg.InitialFields) > 0 {
		fs := make([]zap.Field, 0, len(cfg.InitialFields))
		keys := make([]string, 0, len(cfg.InitialFields))
		for k := range cfg.InitialFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fs = append(fs, zap.Any(k, cfg.InitialFields[k]))
		}
		opts = append(opts, zap.Fields(fs...))
	}

	return opts
}
