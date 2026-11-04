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
	// 1. If a full REDIS_URL is provided (common on Render, Heroku, etc.), parse it directly
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opt, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse REDIS_URL: %w", err)
		}

		client := redis.NewClient(opt)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("failed to connect to redis via URL: %w", err)
		}

		log.Println("Successfully connected to Redis via REDIS_URL!")
		return client, nil
	}

	// 2. Otherwise, fall back to individual host/port variables (used in Docker Compose / Local)
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}

	addr := fmt.Sprintf("%s:%s", redisHost, redisPort)
	password := os.Getenv("REDIS_PASSWORD")

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
