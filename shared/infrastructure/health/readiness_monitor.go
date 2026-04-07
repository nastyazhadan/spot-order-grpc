package health

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/health/grpc_health_v1"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

const (
	defaultCheckInterval    = 10 * time.Second
	defaultCheckTimeout     = 3 * time.Second
	defaultSuccessThreshold = 1
	defaultFailureThreshold = 1
)

type DependencyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type ReadinessMonitorConfig struct {
	ServiceName      string
	ServiceNames     []string
	CheckInterval    time.Duration
	CheckTimeout     time.Duration
	SuccessThreshold int
	FailureThreshold int
}

type ReadinessMonitor struct {
	server       *Server
	logger       *zapLogger.Logger
	cfg          ReadinessMonitorConfig
	dependencies []DependencyCheck

	serving       bool
	successStreak int
	failureStreak int
	lastFailure   string
}

func NewReadinessMonitor(
	server *Server,
	logger *zapLogger.Logger,
	cfg ReadinessMonitorConfig,
	dependencies ...DependencyCheck,
) *ReadinessMonitor {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = defaultCheckInterval
	}
	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = defaultCheckTimeout
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = defaultSuccessThreshold
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaultFailureThreshold
	}

	monitor := &ReadinessMonitor{
		server:       server,
		logger:       logger,
		cfg:          cfg,
		dependencies: dependencies,
	}

	monitor.setStatus(grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	return monitor
}

func (m *ReadinessMonitor) RequireReady(ctx context.Context) error {
	failure, err := m.checkDependencies(ctx)
	if err != nil {
		m.failureStreak = 1
		m.successStreak = 0
		m.lastFailure = failure
		m.setStatus(grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		return err
	}

	m.successStreak = 1
	m.failureStreak = 0
	m.lastFailure = ""
	m.serving = true
	m.setStatus(grpc_health_v1.HealthCheckResponse_SERVING)

	m.logger.Info(ctx, "Service readiness check passed",
		zap.String("service_name", m.cfg.ServiceName),
	)
	return nil
}

func (m *ReadinessMonitor) Watch(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.probe(ctx)
		}
	}
}

func (m *ReadinessMonitor) probe(ctx context.Context) {
	failure, err := m.checkDependencies(ctx)
	if err != nil {
		m.failureStreak++
		m.successStreak = 0
		m.lastFailure = failure

		if m.serving && m.failureStreak >= m.cfg.FailureThreshold {
			m.serving = false
			m.setStatus(grpc_health_v1.HealthCheckResponse_NOT_SERVING)

			m.logger.Warn(ctx, "Service marked as NOT_SERVING",
				zap.String("service_name", m.cfg.ServiceName),
				zap.Int("failure_threshold", m.cfg.FailureThreshold),
				zap.Int("success_threshold", m.cfg.SuccessThreshold),
				zap.String("last_failure", m.lastFailure),
			)
		}

		return
	}

	m.successStreak++
	m.failureStreak = 0
	m.lastFailure = ""

	if !m.serving && m.successStreak >= m.cfg.SuccessThreshold {
		m.serving = true
		m.setStatus(grpc_health_v1.HealthCheckResponse_SERVING)

		m.logger.Info(ctx, "Service marked as SERVING",
			zap.String("service_name", m.cfg.ServiceName),
			zap.Int("failure_threshold", m.cfg.FailureThreshold),
			zap.Int("success_threshold", m.cfg.SuccessThreshold),
		)
	}
}

func (m *ReadinessMonitor) checkDependencies(ctx context.Context) (string, error) {
	var failures []string

	for _, dependency := range m.dependencies {
		depCtx, cancel := context.WithTimeout(ctx, m.cfg.CheckTimeout)
		err := dependency.Check(depCtx)
		cancel()

		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", dependency.Name, err))
		}
	}

	if len(failures) == 0 {
		return "", nil
	}

	failure := strings.Join(failures, "; ")
	return failure, errors.New(failure)
}

func (m *ReadinessMonitor) setStatus(status grpc_health_v1.HealthCheckResponse_ServingStatus) {
	m.server.SetServingStatus("", status)

	for _, serviceName := range m.cfg.ServiceNames {
		if serviceName == "" {
			continue
		}
		m.server.SetServingStatus(serviceName, status)
	}
}
