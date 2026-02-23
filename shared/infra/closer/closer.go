package closer

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"go.uber.org/zap"
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

func (n *NoopLogger) Info(ctx context.Context, message string, fields ...zap.Field)  {}
func (n *NoopLogger) Error(ctx context.Context, message string, fields ...zap.Field) {}

var globalCloser = NewWithLogger(&NoopLogger{})

func AddNamed(name string, function func(context.Context) error) {
	globalCloser.AddNamed(name, function)
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

func (c *Closer) SetLogger(logger Logger) {
	c.logger = logger
}

func (c *Closer) handleSignals(signals ...os.Signal) {
	channel := make(chan os.Signal, 1)
	signal.Notify(channel, signals...)
	defer signal.Stop(channel)

	select {
	case <-channel:
		c.logger.Info(context.Background(), "received shutdown signal")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		if err := c.CloseAll(shutdownCtx); err != nil {
			c.logger.Error(context.Background(), "shutdown error",
				zap.Error(err))
		}

	case <-c.done:
	}
}

func (c *Closer) AddNamed(name string, function func(context.Context) error) {
	c.Add(func(ctx context.Context) error {
		start := time.Now()
		c.logger.Info(ctx, fmt.Sprintf("closing %s...", name))

		err := function(ctx)

		duration := time.Since(start)
		if err != nil {
			c.logger.Error(ctx, fmt.Sprintf("failed to close %s (took %s)", name, duration), zap.Error(err))
		} else {
			c.logger.Info(ctx, fmt.Sprintf("%s closed (took %s)", name, duration))
		}
		return err
	})
}

func (c *Closer) Add(functions ...func(context.Context) error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.funcs = append(c.funcs, functions...)
}

func (c *Closer) CloseAll(ctx context.Context) error {
	var result error

	c.once.Do(func() {
		defer close(c.done)

		c.mutex.Lock()
		funcs := c.funcs
		c.funcs = nil
		c.mutex.Unlock()

		if len(funcs) == 0 {
			c.logger.Info(ctx, "no functions to close")
			return
		}

		c.logger.Info(ctx, "starting graceful shutdown...")

		for i := len(funcs) - 1; i >= 0; i-- {
			if ctx.Err() != nil {
				c.logger.Info(ctx, "context canceled during shutdown",
					zap.Error(ctx.Err()))
				if result == nil {
					result = ctx.Err()
				}
				return
			}

			if err := c.safeRun(ctx, funcs[i]); err != nil {
				c.logger.Error(ctx, "error closing resource",
					zap.Error(err),
				)
				if result == nil {
					result = err
				}
			}
		}
		c.logger.Info(ctx, "all resources closed")
	})

	return result
}

func (c *Closer) safeRun(ctx context.Context, function func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in close function: %v", r)
			c.logger.Error(ctx, "panic recovered during shutdown",
				zap.Any("panic", r),
			)
		}
	}()

	return function(ctx)
}
