package metrics

import (
	"context"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"github.com/prometheus/client_golang/prometheus/push"
	"go.uber.org/zap"
)

func PushShutdownMetric(ctx context.Context, pushGatewayURL, serviceName string) {
	ShutdownsTotal.WithLabelValues(serviceName, "graceful").Inc()

	if err := push.New(pushGatewayURL, "spotorder_shutdowns").
		Collector(ShutdownsTotal).
		Push(); err != nil {
		zapLogger.Error(ctx, "Failed to push shutdown metrics",
			zap.String("service", serviceName),
			zap.Error(err),
		)
	}
}
