package middleware

import (
	"net/http"
	"time"
	"context"
	"fmt"
	"log"
	"net"
	"github.com/redis/go-redis/v9"
)

func RateLimiter(redisClient *redis.Client, limit int, window time.Duration) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				ip = forwarded
			}

			ctx := context.Background()
			key := fmt.Sprintf("ratelimit:%s", ip)

			count, err := redisClient.Incr(ctx, key).Result()
			if err != nil {
				log.Printf("--> ⚠️ [RATE LIMIT ERROR] Redis Incr failed: %v", err)
				next.ServeHTTP(w, r)
				return
			}

			if count == 1 {
				redisClient.Expire(ctx, key, window)
				log.Printf("--> ⏱️ [RATE LIMIT] First request for IP %s. Set TTL to %v", ip, window)
			}

			log.Printf("--> 🔍 [RATE LIMIT CHECK] IP: %s | Count: %d / %d", ip, count, limit)

			if count > int64(limit) {
				log.Printf("--> 🚫 [RATE LIMIT EXCEEDED] Blocking IP %s (Count: %d)", ip, count)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				http.Error(w, "Rate limit exceeded. Too many requests, please try again later.", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		}
	}
}

//func RateLimiter(redisClient *redis.Client, limit int, window time.Duration) func(http.HandlerFunc) http.HandlerFunc {
//	return func(next http.HandlerFunc) http.HandlerFunc {
//		return func(w http.ResponseWriter, r *http.Request) {
//			ctx := r.Context()
//			ip := r.RemoteAddr
//
//			key := "ratelimit:" + ip
//			count, err := redisClient.Incr(ctx, key).Result()
//			if err != nil {
//				// Fail open: if Redis errors, allow the request rather than blocking users
//				next(w, r)
//				return
//			}
//
//			// Set expiration window on the first request from this IP
//			if count == 1 {
//				_ = redisClient.Expire(ctx, key, window).Err()
//			}
//
//			if count > int64(limit) {
//				http.Error(w, "Rate limit exceeded. Too many requests.", http.StatusTooManyRequests)
//				return
//			}
//
//			next(w, r)
//		}
//	}
//}
