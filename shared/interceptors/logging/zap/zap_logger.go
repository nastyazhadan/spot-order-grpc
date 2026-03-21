package zap

import (
	"context"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

type contextKey string

const (
	extraFieldsKey contextKey = "log_extra_fields"
)

type extraFields struct {
	parent *extraFields
	fields []zap.Field
	total  int
}

type Logger struct {
	zapLogger    *zap.Logger
	dynamicLevel zap.AtomicLevel
}

func New(levelStr string, asJSON bool) *Logger {
	level := zap.NewAtomicLevelAt(parseLevel(levelStr))

	encoderCfg := buildEncoderConfig()

	var encoder zapcore.Encoder
	if asJSON {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(3))

	return &Logger{
		zapLogger:    logger,
		dynamicLevel: level,
	}
}

func NewNop() *Logger {
	return &Logger{
		zapLogger: zap.NewNop(),
	}
}

func buildEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logging",
		CallerKey:      "caller",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeName:     zapcore.FullNameEncoder,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
	}
}

func (l *Logger) SetLevel(levelStr string) {
	if l == nil {
		return
	}

	l.dynamicLevel.SetLevel(parseLevel(levelStr))
}

func (l *Logger) Sync() error {
	if l == nil || l.zapLogger == nil {
		return nil
	}

	err := l.zapLogger.Sync()
	if err != nil && isIgnorableSyncError(err) {
		return nil
	}

	return err
}

func isIgnorableSyncError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "sync /dev/stdout: invalid argument") ||
		strings.Contains(msg, "sync /dev/stderr: invalid argument") ||
		strings.Contains(msg, "inappropriate ioctl for device")
}

func (l *Logger) With(fields ...zap.Field) *Logger {
	if l == nil || l.zapLogger == nil {
		return NewNop()
	}
	if len(fields) == 0 {
		return l
	}

	return &Logger{
		zapLogger:    l.zapLogger.With(fields...),
		dynamicLevel: l.dynamicLevel,
	}
}

func WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	if len(fields) == 0 {
		return ctx
	}

	parent, _ := ctx.Value(extraFieldsKey).(*extraFields)

	node := &extraFields{
		parent: parent,
		fields: fields,
		total:  len(fields),
	}
	if parent != nil {
		node.total += parent.total
	}

	return context.WithValue(ctx, extraFieldsKey, node)
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	if l == nil || l.zapLogger == nil {
		return NewNop()
	}

	ctxFields := fieldsFromContext(ctx)
	if len(ctxFields) == 0 {
		return l
	}

	return &Logger{
		zapLogger:    l.zapLogger.With(ctxFields...),
		dynamicLevel: l.dynamicLevel,
	}
}

func (l *Logger) Debug(ctx context.Context, message string, fields ...zap.Field) {
	l.log(ctx, zapcore.DebugLevel, message, fields...)
}

func (l *Logger) Info(ctx context.Context, message string, fields ...zap.Field) {
	l.log(ctx, zapcore.InfoLevel, message, fields...)
}

func (l *Logger) Warn(ctx context.Context, message string, fields ...zap.Field) {
	l.log(ctx, zapcore.WarnLevel, message, fields...)
}

func (l *Logger) Error(ctx context.Context, message string, fields ...zap.Field) {
	l.log(ctx, zapcore.ErrorLevel, message, fields...)
}

func (l *Logger) Fatal(ctx context.Context, message string, fields ...zap.Field) {
	l.log(ctx, zapcore.FatalLevel, message, fields...)
}

func (l *Logger) log(ctx context.Context, level zapcore.Level, message string, fields ...zap.Field) {
	if l == nil || l.zapLogger == nil {
		return
	}

	checked := l.zapLogger.Check(level, message)
	if checked == nil {
		return
	}

	ctxFields := fieldsFromContext(ctx)

	switch {
	case len(ctxFields) == 0:
		checked.Write(fields...)
	case len(fields) == 0:
		checked.Write(ctxFields...)
	default:
		merged := make([]zap.Field, 0, len(ctxFields)+len(fields))
		merged = append(merged, ctxFields...)
		merged = append(merged, fields...)
		checked.Write(merged...)
	}
}

func fieldsFromContext(ctx context.Context) []zap.Field {
	traceID, hasTraceID := requestctx.TraceIDFromContext(ctx)
	userID, hasUserID := requestctx.UserIDFromContext(ctx)
	extra, _ := ctx.Value(extraFieldsKey).(*extraFields)

	totalFields := 0
	if hasTraceID {
		totalFields++
	}
	if hasUserID {
		totalFields++
	}
	if extra != nil {
		totalFields += extra.total
	}

	if totalFields == 0 {
		return nil
	}

	fields := make([]zap.Field, 0, totalFields)

	if hasTraceID {
		fields = append(fields, zap.String(requestctx.TraceIDField, traceID))
	}
	if hasUserID {
		fields = append(fields, zap.String("user_id", userID.String()))
	}
	if extra != nil {
		fields = appendExtraFields(fields, extra)
	}

	return fields
}

func appendExtraFields(fields []zap.Field, node *extraFields) []zap.Field {
	if node == nil {
		return fields
	}
	if node.parent != nil {
		fields = appendExtraFields(fields, node.parent)
	}

	return append(fields, node.fields...)
}

func parseLevel(levelString string) zapcore.Level {
	switch strings.ToLower(levelString) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
