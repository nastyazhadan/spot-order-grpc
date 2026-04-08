package producer

import (
	"context"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
)

type producer struct {
	client *Client
	topic  string
}

func New(client *Client, topic string) *producer {
	return &producer{
		client: client,
		topic:  topic,
	}
}

func (p *producer) Send(ctx context.Context, key, value []byte) error {
	return p.SendMessage(ctx, kafka.Message{
		Key:   key,
		Value: value,
	})
}

func (p *producer) SendMessage(ctx context.Context, message kafka.Message) error {
	return p.client.publish(ctx, p.topic, message)
}
