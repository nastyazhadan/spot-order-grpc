package metrics

import (
	"context"
	"fmt"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc/credentials/insecure"
)

func InitOpenTelemetry(
	ctx context.Context,
	metricsCfg config.MetricsConfig,
	tracingCfg config.TracingConfig,
) (*sdkmetric.MeterProvider, error) {
	exporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint(tracingCfg.CollectorEndpoint),
		otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OpenTelemetry exporter: %w", err)
	}

	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			attribute.String("service.name", tracingCfg.ServiceName),
			attribute.String("service.version", tracingCfg.ServiceVersion),
			attribute.String("deployment.environment", tracingCfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create otel resource: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				exporter,
				sdkmetric.WithInterval(metricsCfg.ExportInterval),
			),
		),
	)

	otel.SetMeterProvider(meterProvider)
	zapLogger.Info(ctx, "OpenTelemetry metrics initialized successfully")

	return meterProvider, nil
}
