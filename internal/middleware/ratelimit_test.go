package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"brevity/internal/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRateLimiterMiddleware(t *testing.T) {
	// 1. Start a mock in-memory Redis server
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	// 2. Connect a go-redis client to the mock server
	rClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// 3. Define a dummy next handler that returns 200 OK
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// Wrap with RateLimiter: Limit to 2 requests per 1 minute
	limitedHandler := middleware.RateLimiter(rClient, 2, 1*time.Minute)(nextHandler)

	// 4. Request 1: Should pass (200 OK)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/shorten", nil)
	req1.RemoteAddr = "192.168.1.1:1234"
	rec1 := httptest.NewRecorder()
	limitedHandler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("expected status 200 for request 1; got %d", rec1.Code)
	}

	// 5. Request 2: Should pass (200 OK)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/shorten", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	rec2 := httptest.NewRecorder()
	limitedHandler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected status 200 for request 2; got %d", rec2.Code)
	}

	// 6. Request 3: Exceeds limit of 2 -> Should return 429 Too Many Requests
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/shorten", nil)
	req3.RemoteAddr = "192.168.1.1:1234"
	rec3 := httptest.NewRecorder()
	limitedHandler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429 for request 3; got %d", rec3.Code)
	}
}
