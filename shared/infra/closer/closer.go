package closer

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"go.uber.org/zap"

	logger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
)

const shutdownTimeout = 5 * time.Second

type Logger interface {
	Info(ctx context.Context, msg string, fields ...zap.Field)
	Error(ctx context.Context, msg string, fields ...zap.Field)
}

type Closer struct {
	mutex  sync.Mutex
	once   sync.Once
	done   chan struct{}
	funcs  []func(context.Context) error
	logger Logger
}

type NoopLogger struct{}

func (logger *NoopLogger) Info(ctx context.Context, message string, fields ...zap.Field)  {}
func (logger *NoopLogger) Error(ctx context.Context, message string, fields ...zap.Field) {}

var globalCloser = NewWithLogger(&NoopLogger{})

func AddNamed(name string, function func(context.Context) error) {
	globalCloser.AddNamed(name, function)
}

func Add(functions ...func(context.Context) error) {
	globalCloser.Add(functions...)
}

func CloseAll(ctx context.Context) error {
	return globalCloser.CloseAll(ctx)
}

func SetLogger(logger Logger) {
	globalCloser.SetLogger(logger)
}

func Configure(signals ...os.Signal) {
	go globalCloser.handleSignals(signals...)
}

func New(signals ...os.Signal) *Closer {
	return NewWithLogger(logger.Logger(), signals...)
}

func NewWithLogger(logger Logger, signals ...os.Signal) *Closer {
	closer := &Closer{
		done:   make(chan struct{}),
		logger: logger,
	}

	if len(signals) > 0 {
		go closer.handleSignals(signals...)
	}

	return closer
}

func (closer *Closer) SetLogger(logger Logger) {
	closer.logger = logger
}

func (closer *Closer) handleSignals(signals ...os.Signal) {
	channel := make(chan os.Signal, 1)
	signal.Notify(channel, signals...)
	defer signal.Stop(channel)

	select {
	case <-channel:
		closer.logger.Info(context.Background(), "received shutdown signal")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		if err := closer.CloseAll(shutdownCtx); err != nil {
			closer.logger.Error(context.Background(), "shutdown error",
				zap.Error(err))
		}

	case <-closer.done:
	}
}

func (closer *Closer) AddNamed(name string, function func(context.Context) error) {
	closer.Add(func(ctx context.Context) error {
		start := time.Now()
		closer.logger.Info(ctx, fmt.Sprintf("closing %s...", name))

		err := function(ctx)

		duration := time.Since(start)
		if err != nil {
			closer.logger.Error(ctx, fmt.Sprintf("failed to close %s (took %s)", name, duration), zap.Error(err))
		} else {
			closer.logger.Info(ctx, fmt.Sprintf("%s closed (took %s)", name, duration))
		}
		return err
	})
}

func (closer *Closer) Add(functions ...func(context.Context) error) {
	closer.mutex.Lock()
	defer closer.mutex.Unlock()

	closer.funcs = append(closer.funcs, functions...)
}

func (closer *Closer) CloseAll(ctx context.Context) error {
	var result error

	closer.once.Do(func() {
		defer close(closer.done)

		closer.mutex.Lock()
		funcs := closer.funcs
		closer.funcs = nil
		closer.mutex.Unlock()

		if len(funcs) == 0 {
			closer.logger.Info(ctx, "no functions to close")
			return
		}

		closer.logger.Info(ctx, "starting graceful shutdown...")

		for i := len(funcs) - 1; i >= 0; i-- {
			if ctx.Err() != nil {
				closer.logger.Info(ctx, "context canceled during shutdown",
					zap.Error(ctx.Err()))
				if result == nil {
					result = ctx.Err()
				}
				return
			}

			if err := closer.safeRun(ctx, funcs[i]); err != nil {
				closer.logger.Error(ctx, "error closing resource",
					zap.Error(err),
				)
				if result == nil {
					result = err
				}
			}
		}
		closer.logger.Info(ctx, "all resources closed")
	})

	return result
}

func (closer *Closer) safeRun(ctx context.Context, function func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in close function: %v", r)
			closer.logger.Error(ctx, "panic recovered during shutdown",
				zap.Any("panic", r),
			)
		}
	}()

	return function(ctx)
}
