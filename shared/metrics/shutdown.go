package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus/push"
)

func PushShutdownMetric(ctx context.Context, pushGatewayURL, serviceName string) {
	ShutdownsTotal.WithLabelValues(serviceName, "graceful").Inc()

	if err := push.New(pushGatewayURL, "spotorder_shutdowns").
		Collector(ShutdownsTotal).
		Push(); err != nil {
	}
}
