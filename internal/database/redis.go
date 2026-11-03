package database

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient(addr string) (*redis.Client, error) {
	// Securely load the Redis password from environment variables
	password := os.Getenv("REDIS_PASSWORD")

	client := redis.NewClient(&redis.Options{
        Addr:     addr,
        Password: password,
        DB:       0,
		TLSConfig: &tls.Config{
            MinVersion: tls.VersionTLS12,
        },
    })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	log.Println("Successfully connected to Redis cache for Brevity!")
	return client, nil
}
