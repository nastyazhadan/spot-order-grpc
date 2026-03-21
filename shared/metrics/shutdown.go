package metrics

func RecordShutdown(serviceName string) {
	ShutdownsTotal.WithLabelValues(serviceName, "graceful").Inc()
}
