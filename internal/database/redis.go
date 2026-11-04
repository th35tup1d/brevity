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

func NewRedisClient() (*redis.Client, error) {
	// Load host and port from environment variables with sensible defaults
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost" // Fallback for local non-Docker development
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379" // Default Redis port
	}

	addr := fmt.Sprintf("%s:%s", redisHost, redisPort)

	// Securely load the Redis password from environment variables
	password := os.Getenv("REDIS_PASSWORD")

	// Only enable TLS if running in production or explicitly required
	var tlsConfig *tls.Config
	if os.Getenv("ENV") == "production" {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	client := redis.NewClient(&redis.Options{
		Addr:      addr,
		Password:  password,
		DB:        0,
		TLSConfig: tlsConfig,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis at %s: %w", addr, err)
	}

	log.Println("Successfully connected to Redis cache for Brevity!")
	return client, nil
}
