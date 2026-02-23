package redis

import (
	"context"
	"time"

	redigo "github.com/gomodule/redigo/redis"
	"go.uber.org/zap"
)

type client struct {
	pool              *redigo.Pool
	logger            Logger
	connectionTimeout time.Duration
}

type Logger interface {
	Info(ctx context.Context, message string, fields ...zap.Field)
	Error(ctx context.Context, message string, fields ...zap.Field)
}

type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}) error
	SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	HashSet(ctx context.Context, key string, value interface{}) error
	HGetAll(ctx context.Context, key string) ([]any, error)
	Del(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Expire(ctx context.Context, key string, expirationTime time.Duration) error
	Ping(ctx context.Context) error
}

type redisFn func(ctx context.Context, conn redigo.Conn) error

func NewClient(pool *redigo.Pool, logger Logger, connectionTimeout time.Duration) *client {
	return &client{
		pool:              pool,
		logger:            logger,
		connectionTimeout: connectionTimeout,
	}
}

func (c *client) withConn(ctx context.Context, fn redisFn) error {
	connection, err := c.getConn(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if cErr := connection.Close(); cErr != nil {
			c.logger.Error(ctx, "failed to close client connection",
				zap.Error(cErr),
			)
		}
	}()

	return fn(ctx, connection)
}

func (c *client) getConn(ctx context.Context) (redigo.Conn, error) {
	connCtx, cancel := context.WithTimeout(ctx, c.connectionTimeout)
	defer cancel()

	connection, err := c.pool.GetContext(connCtx)
	if err != nil {
		c.logger.Error(ctx, "failed to get client connection",
			zap.Error(err),
		)
		return nil, err
	}

	return connection, nil
}

func (c *client) Set(ctx context.Context, key string, value interface{}) error {
	return c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		_, err := conn.Do("SET", key, value)
		return err
	})
}

func (c *client) SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		_, err := conn.Do("SET", key, value, "EX", int(ttl.Seconds()))
		return err
	})
}

func (c *client) Get(ctx context.Context, key string) ([]byte, error) {
	var result []byte
	err := c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		value, err := redigo.Bytes(conn.Do("GET", key))
		if err != nil {
			return err
		}

		result = value
		return nil
	})

	return result, err
}

func (c *client) HashSet(ctx context.Context, key string, values interface{}) error {
	return c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		_, err := conn.Do("HSET", redigo.Args{key}.AddFlat(values)...)
		return err
	})
}

func (c *client) HGetAll(ctx context.Context, key string) ([]any, error) {
	var values []any
	err := c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		result, err := redigo.Values(conn.Do("HGETALL", key))
		if err != nil {
			return err
		}

		values = result
		return nil
	})

	return values, err
}

func (c *client) Del(ctx context.Context, key string) error {
	return c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		_, err := conn.Do("DEL", key)
		return err
	})
}

func (c *client) Exists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		value, err := redigo.Bool(conn.Do("EXISTS", key))
		if err != nil {
			return err
		}

		exists = value
		return nil
	})

	return exists, err
}

func (c *client) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		_, err := conn.Do("EXPIRE", key, int(expiration.Seconds()))
		return err
	})
}

func (c *client) Ping(ctx context.Context) error {
	return c.withConn(ctx, func(ctx context.Context, conn redigo.Conn) error {
		_, err := conn.Do("PING")
		return err
	})
}
