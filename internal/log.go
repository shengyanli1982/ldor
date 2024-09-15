package internal

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	zapLogger *zap.Logger
}

func formatLogEntry(entry interface{}, encoder zapcore.PrimitiveArrayEncoder) {
	switch typedEntry := entry.(type) {
	case time.Time:
		encoder.AppendString(typedEntry.Format("2006-01-02T15:04:05.000Z0700") + " |")
	case zapcore.EntryCaller:
		encoder.AppendString(typedEntry.TrimmedPath() + " |")
	case time.Duration:
		encoder.AppendString(typedEntry.String() + " |")
	case zapcore.Level:
		encoder.AppendString(typedEntry.CapitalString() + " |")
	case string:
		encoder.AppendString(typedEntry + " |")
	default:
		encoder.AppendString(fmt.Sprintf("%v |", typedEntry))
	}
}

var CustomTextLogEncoderConfig = zapcore.EncoderConfig{
	TimeKey:        "time",
	LevelKey:       "level",
	NameKey:        "logger",
	CallerKey:      "caller",
	MessageKey:     "msg",
	StacktraceKey:  "stacktrace",
	LineEnding:     zapcore.DefaultLineEnding,
	EncodeTime:     func(t time.Time, enc zapcore.PrimitiveArrayEncoder) { formatLogEntry(t, enc) },
	EncodeDuration: func(d time.Duration, enc zapcore.PrimitiveArrayEncoder) { formatLogEntry(d, enc) },
	EncodeCaller:   func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) { formatLogEntry(caller, enc) },
	EncodeLevel:    func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) { formatLogEntry(level, enc) },
	EncodeName:     func(name string, enc zapcore.PrimitiveArrayEncoder) { formatLogEntry(name, enc) },
}

func NewLogger(writeSyncer zapcore.WriteSyncer, options ...zap.Option) *Logger {
	if writeSyncer == nil {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	logCore := zapcore.NewCore(zapcore.NewConsoleEncoder(CustomTextLogEncoderConfig), writeSyncer, zap.NewAtomicLevelAt(zap.DebugLevel))
	return &Logger{zapLogger: zap.New(logCore, zap.AddCaller()).WithOptions(options...)}
}

func (cl *Logger) GetZapSugaredLogger() *zap.SugaredLogger {
	return cl.zapLogger.Sugar()
}
