package internal

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	l *zap.Logger
}

func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000") + " |")
}

func customCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(caller.TrimmedPath() + " |")
}

func customDurationEncoder(d time.Duration, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(d.String() + " |")
}

func customLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(level.CapitalString() + " |")
}

func customNameEncoder(name string, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(name + " |")
}

var PlainTextLogEncodingConfig = zapcore.EncoderConfig{
	TimeKey:        "time",
	LevelKey:       "level",
	NameKey:        "logger",
	CallerKey:      "caller",
	MessageKey:     "msg",
	StacktraceKey:  "stacktrace",
	LineEnding:     zapcore.DefaultLineEnding,
	EncodeTime:     customTimeEncoder,
	EncodeDuration: customDurationEncoder,
	EncodeCaller:   customCallerEncoder,
	EncodeLevel:    customLevelEncoder,
	EncodeName:     customNameEncoder,
}

func NewLogger(ws zapcore.WriteSyncer, opts ...zap.Option) *Logger {
	if ws == nil {
		ws = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(PlainTextLogEncodingConfig), ws, zap.NewAtomicLevelAt(zap.DebugLevel))
	return &Logger{l: zap.New(core, zap.AddCaller()).WithOptions(opts...)}
}

func (l *Logger) GetZapSugaredLogger() *zap.SugaredLogger {
	return l.l.Sugar()
}
