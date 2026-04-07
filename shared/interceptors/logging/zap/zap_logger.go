package zap

import (
	"context"
	"os"
	"slices"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

type contextKey string

const (
	extraFieldsKey contextKey = "log_extra_fields"
)

type contextFields struct {
	fields []zap.Field
}

type Logger struct {
	zapLogger    *zap.Logger
	dynamicLevel zap.AtomicLevel
	// ограничивает только extra fields
	contextFieldsMax int
}

func New(levelStr string, asJSON bool, contextFieldsMax int) *Logger {
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

	baseLogger := zap.New(core, zap.AddCaller())
	logger := baseLogger.WithOptions(zap.AddCallerSkip(1))

	return &Logger{
		zapLogger:        logger,
		dynamicLevel:     level,
		contextFieldsMax: normalizeContextFieldsMax(contextFieldsMax),
	}
}

func NewNop() *Logger {
	return &Logger{
		zapLogger:        zap.NewNop(),
		contextFieldsMax: 0,
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

func normalizeContextFieldsMax(limit int) int {
	if limit < 0 {
		return 0
	}
	return limit
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

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "sync /dev/stdout: invalid argument") ||
		strings.Contains(message, "sync /dev/stderr: invalid argument") ||
		strings.Contains(message, "inappropriate ioctl for device")
}

func (l *Logger) With(fields ...zap.Field) *Logger {
	if l == nil || l.zapLogger == nil {
		return NewNop()
	}
	if len(fields) == 0 {
		return l
	}

	return &Logger{
		zapLogger:        l.zapLogger.With(fields...),
		dynamicLevel:     l.dynamicLevel,
		contextFieldsMax: l.contextFieldsMax,
	}
}

func (l *Logger) WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	if l == nil || l.contextFieldsMax <= 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}

	current, _ := ctx.Value(extraFieldsKey).(*contextFields)
	existingCount := 0
	if current != nil {
		existingCount = len(current.fields)
	}

	available := l.contextFieldsMax - existingCount
	if available <= 0 {
		return ctx
	}

	toAdd := fields
	if len(toAdd) > available {
		toAdd = toAdd[:available]
	}

	if len(toAdd) == 0 {
		return ctx
	}

	merged := make([]zap.Field, 0, existingCount+len(toAdd))
	if current != nil {
		merged = append(merged, current.fields...)
	}
	merged = append(merged, toAdd...)

	return context.WithValue(ctx, extraFieldsKey, &contextFields{
		fields: merged,
	})
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	if l == nil || l.zapLogger == nil {
		return NewNop()
	}

	ctxFields := fieldsFromContext(ctx)
	if len(ctxFields) == 0 {
		return l
	}

	return l.With(ctxFields...)
}

func (l *Logger) Debug(ctx context.Context, message string, fields ...zap.Field) {
	if l == nil || l.zapLogger == nil {
		return
	}

	l.zapLogger.Debug(message, l.mergeFields(ctx, fields...)...)
}

func (l *Logger) Info(ctx context.Context, message string, fields ...zap.Field) {
	if l == nil || l.zapLogger == nil {
		return
	}

	l.zapLogger.Info(message, l.mergeFields(ctx, fields...)...)
}

func (l *Logger) Warn(ctx context.Context, message string, fields ...zap.Field) {
	if l == nil || l.zapLogger == nil {
		return
	}

	l.zapLogger.Warn(message, l.mergeFields(ctx, fields...)...)
}

func (l *Logger) Error(ctx context.Context, message string, fields ...zap.Field) {
	if l == nil || l.zapLogger == nil {
		return
	}

	l.zapLogger.Error(message, l.mergeFields(ctx, fields...)...)
}

func (l *Logger) Fatal(ctx context.Context, message string, fields ...zap.Field) {
	if l == nil || l.zapLogger == nil {
		return
	}

	l.zapLogger.Fatal(message, l.mergeFields(ctx, fields...)...)
}

func (l *Logger) mergeFields(ctx context.Context, fields ...zap.Field) []zap.Field {
	ctxFields := fieldsFromContext(ctx)

	switch {
	case len(ctxFields) == 0:
		return fields
	case len(fields) == 0:
		return ctxFields
	default:
		merged := make([]zap.Field, 0, len(ctxFields)+len(fields))
		merged = append(merged, ctxFields...)
		merged = append(merged, fields...)
		return merged
	}
}

func fieldsFromContext(ctx context.Context) []zap.Field {
	if ctx == nil {
		return nil
	}

	traceID, hasTraceID := requestctx.TraceIDFromContext(ctx)
	userID, hasUserID := requestctx.UserIDFromContext(ctx)
	userRoles, hasUserRoles := requestctx.UserRolesFromContext(ctx)
	extra, _ := ctx.Value(extraFieldsKey).(*contextFields)

	totalFields := 0
	if hasTraceID {
		totalFields++
	}
	if hasUserID {
		totalFields++
	}
	if hasUserRoles {
		totalFields++
	}
	if extra != nil {
		totalFields += len(extra.fields)
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
	if hasUserRoles {
		roleNames := make([]string, 0, len(userRoles))
		for _, role := range userRoles {
			roleNames = append(roleNames, role.String())
		}
		slices.Sort(roleNames)
		fields = append(fields, zap.Strings("user_roles", roleNames))
	}
	if extra != nil {
		fields = append(fields, extra.fields...)
	}

	return fields
}

func parseLevel(levelString string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(levelString)) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}
