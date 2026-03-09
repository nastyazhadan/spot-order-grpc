package tracing

import "google.golang.org/grpc/metadata"

type metadataCarrier metadata.MD

func (mc metadataCarrier) Get(key string) string {
	values := metadata.MD(mc).Get(key)
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

func (mc metadataCarrier) Set(key, value string) {
	metadata.MD(mc).Set(key, value)
}

func (mc metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(mc))
	for key := range metadata.MD(mc) {
		keys = append(keys, key)
	}

	return keys
}
