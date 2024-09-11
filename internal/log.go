package internal

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.Logger
type Logger struct {
	l *zap.Logger
}

// customEncoder handles custom encoding for various types
func customEncoder(value interface{}, enc zapcore.PrimitiveArrayEncoder) {
	switch v := value.(type) {
	case time.Time:
		enc.AppendString(v.Format("2006-01-02 15:04:05.000") + " |")
	case zapcore.EntryCaller:
		enc.AppendString(v.TrimmedPath() + " |")
	case time.Duration:
		enc.AppendString(v.String() + " |")
	case zapcore.Level:
		enc.AppendString(v.CapitalString() + " |")
	case string:
		enc.AppendString(v + " |")
	default:
		enc.AppendString(fmt.Sprintf("%v |", v))
	}
}

// PlainTextLogEncodingConfig defines the encoding configuration for plain text logs
var PlainTextLogEncodingConfig = zapcore.EncoderConfig{
	TimeKey:        "time",
	LevelKey:       "level",
	NameKey:        "logger",
	CallerKey:      "caller",
	MessageKey:     "msg",
	StacktraceKey:  "stacktrace",
	LineEnding:     zapcore.DefaultLineEnding,
	EncodeTime:     func(t time.Time, enc zapcore.PrimitiveArrayEncoder) { customEncoder(t, enc) },
	EncodeDuration: func(d time.Duration, enc zapcore.PrimitiveArrayEncoder) { customEncoder(d, enc) },
	EncodeCaller:   func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) { customEncoder(caller, enc) },
	EncodeLevel:    func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) { customEncoder(level, enc) },
	EncodeName:     func(name string, enc zapcore.PrimitiveArrayEncoder) { customEncoder(name, enc) },
}

// NewLogger creates a new Logger instance
func NewLogger(ws zapcore.WriteSyncer, opts ...zap.Option) *Logger {
	if ws == nil {
		ws = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(PlainTextLogEncodingConfig),
		ws,
		zap.NewAtomicLevelAt(zap.DebugLevel),
	)
	return &Logger{l: zap.New(core, zap.AddCaller()).WithOptions(opts...)}
}

// GetZapSugaredLogger returns a SugaredLogger instance
func (l *Logger) GetZapSugaredLogger() *zap.SugaredLogger {
	return l.l.Sugar()
}
