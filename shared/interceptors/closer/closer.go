package closer

import (
	"context"
	"errors"
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
		closer.logger.Info(context.Background(), "Получен системный сигнал, начинаем graceful shutdown...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		if err := closer.CloseAll(shutdownCtx); err != nil {
			closer.logger.Error(context.Background(), "Ошибка при закрытии ресурсов: %v",
				zap.Error(err))
		}

	case <-closer.done:
	}
}

func (closer *Closer) AddNamed(name string, function func(context.Context) error) {
	closer.Add(func(ctx context.Context) error {
		start := time.Now()
		closer.logger.Info(ctx, fmt.Sprintf("Закрываем %s...", name))

		err := function(ctx)

		duration := time.Since(start)
		if err != nil {
			closer.logger.Error(ctx, fmt.Sprintf("Ошибка при закрытии %s: %v (заняло %s)", name, err, duration))
		} else {
			closer.logger.Info(ctx, fmt.Sprintf("%s успешно закрыт за %s", name, duration))
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
			closer.logger.Info(ctx, "Нет функций для закрытия.")
			return
		}

		closer.logger.Info(ctx, "Начинаем процесс graceful shutdown...")

		errChannel := make(chan error, len(funcs))
		var wg sync.WaitGroup

		for i := len(funcs) - 1; i >= 0; i-- {
			function := funcs[i]
			wg.Add(1)
			go func(function func(context.Context) error) {
				defer wg.Done()

				defer func() {
					if r := recover(); r != nil {
						errChannel <- errors.New("panic recovered in closer")
						closer.logger.Error(ctx, "Panic в функции закрытия", zap.Any("error", r))
					}
				}()

				if err := function(ctx); err != nil {
					errChannel <- err
				}
			}(function)
		}

		go func() {
			wg.Wait()
			close(errChannel)
		}()

		for {
			select {
			case <-ctx.Done():
				closer.logger.Info(ctx, "Контекст отменён во время закрытия", zap.Error(ctx.Err()))
				if result == nil {
					result = ctx.Err()
				}
				return
			case err, ok := <-errChannel:
				if !ok {
					closer.logger.Info(ctx, "Все ресурсы успешно закрыты")
					return
				}
				closer.logger.Error(ctx, "Ошибка при закрытии", zap.Error(err))
				if result == nil {
					result = err
				}
			}
		}
	})

	return result
}
