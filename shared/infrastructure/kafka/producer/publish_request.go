package producer

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type publishRequest struct {
	ctx       context.Context
	span      trace.Span
	topic     string
	started   time.Time
	hasKey    bool
	keyLength int

	done   chan struct{}
	once   sync.Once
	result publishResult
}

func (r *publishRequest) finish(result publishResult) {
	r.once.Do(func() {
		r.result = result
		r.span.End()
		close(r.done)
	})
}

func (r *publishRequest) isDone() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}
