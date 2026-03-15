package zap

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type contextKey string

const (
	TraceIDKey     contextKey = "x-trace-id"
	UserIDKey      contextKey = "user_id"
	extraFieldsKey contextKey = "log_extra_fields"
)

func WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	existing, _ := ctx.Value(extraFieldsKey).([]zap.Field)

	merged := make([]zap.Field, len(existing), len(existing)+len(fields))
	copy(merged, existing)
	merged = append(merged, fields...)

	return context.WithValue(ctx, extraFieldsKey, merged)
}

var (
	globalLogger *logger
	initOnce     sync.Once
	dynamicLevel zap.AtomicLevel
)

type logger struct {
	zapLogger *zap.Logger
}

func Init(levelStr string, asJSON bool) {
	initOnce.Do(func() {
		dynamicLevel = zap.NewAtomicLevelAt(parseLevel(levelStr))

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
			dynamicLevel,
		)

		zapLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(2))

		globalLogger = &logger{
			zapLogger: zapLogger,
		}
	})
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

func Logger() *logger {
	return globalLogger
}

func SetLevel(levelStr string) {
	if dynamicLevel == (zap.AtomicLevel{}) {
		return
	}

	dynamicLevel.SetLevel(parseLevel(levelStr))
}

func SetNopLogger() {
	globalLogger = &logger{zapLogger: zap.NewNop()}
}

func Sync() error {
	if globalLogger == nil || globalLogger.zapLogger == nil {
		return nil
	}

	err := globalLogger.zapLogger.Sync()
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

func With(fields ...zap.Field) *logger {
	if globalLogger == nil {
		return &logger{zapLogger: zap.NewNop()}
	}
	return &logger{zapLogger: globalLogger.zapLogger.With(fields...)}
}

func WithContext(ctx context.Context) *logger {
	if globalLogger == nil {
		return &logger{zapLogger: zap.NewNop()}
	}
	return &logger{zapLogger: globalLogger.zapLogger.With(fieldsFromContext(ctx)...)}
}

func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if value, found := ctx.Value(TraceIDKey).(string); found {
		return value
	}
	return ""
}

func Debug(ctx context.Context, message string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Debug(ctx, message, fields...)
	}
}

func Info(ctx context.Context, message string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Info(ctx, message, fields...)
	}
}

func Warn(ctx context.Context, message string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Warn(ctx, message, fields...)
	}
}

func Error(ctx context.Context, message string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Error(ctx, message, fields...)
	}
}

func Fatal(ctx context.Context, message string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Fatal(ctx, message, fields...)
	}
}

func (l *logger) Debug(ctx context.Context, message string, fields ...zap.Field) {
	l.zapLogger.Debug(message, append(fieldsFromContext(ctx), fields...)...)
}

func (l *logger) Info(ctx context.Context, message string, fields ...zap.Field) {
	l.zapLogger.Info(message, append(fieldsFromContext(ctx), fields...)...)
}

func (l *logger) Warn(ctx context.Context, message string, fields ...zap.Field) {
	l.zapLogger.Warn(message, append(fieldsFromContext(ctx), fields...)...)
}

func (l *logger) Error(ctx context.Context, message string, fields ...zap.Field) {
	l.zapLogger.Error(message, append(fieldsFromContext(ctx), fields...)...)
}

func (l *logger) Fatal(ctx context.Context, message string, fields ...zap.Field) {
	l.zapLogger.Fatal(message, append(fieldsFromContext(ctx), fields...)...)
}

func fieldsFromContext(ctx context.Context) []zap.Field {
	var fields []zap.Field

	if traceID, found := ctx.Value(TraceIDKey).(string); found && traceID != "" {
		fields = append(fields, zap.String(string(TraceIDKey), traceID))
	}

	if userID, found := ctx.Value(UserIDKey).(uuid.UUID); found {
		fields = append(fields, zap.String("user_id", userID.String()))
	}

	if extra, found := ctx.Value(extraFieldsKey).([]zap.Field); found {
		fields = append(fields, extra...)
	}

	return fields
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
