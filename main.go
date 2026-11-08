package main

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	
	"github.com/segmentio/kafka-go"
	"brevity/internal/config"
	"brevity/internal/database"
	"brevity/internal/handlers"
	"brevity/internal/middleware"
	"brevity/internal/repository"
	"brevity/internal/service"
)

//go:embed web/*
var contentEmbed embed.FS

func main() {
	// 1. Load configuration variables
	cfg := config.LoadConfig()

	// 2. Initialize PostgreSQL database connection pool
	pgPool, err := database.NewPostgresDB(cfg.DBConnStr)
	if err != nil {
		log.Fatalf("PostgreSQL connection error: %v", err)
	}
	defer pgPool.Close()

	// 3. Initialize Redis client
	redisClient, err := database.NewRedisClient()
	if err != nil {
		log.Fatalf("Redis connection error: %v", err)
	}
	defer redisClient.Close()

	// 4. Initialize Repositories & Services
	urlRepo := repository.NewURLRepository(pgPool)
	analyticsPool := service.NewAnalyticsWorkerPool(urlRepo, 10000, 5)
	defer analyticsPool.Stop()

	urlSvc := service.NewURLService(urlRepo, redisClient)
	urlHandler := handlers.NewURLHandler(urlSvc, pgPool, redisClient)

	ctx := context.Background()
	urlSvc.StartCleanupWorker(ctx, 1*time.Hour)

	// Initialize and start Kafka Analytics Consumer Worker
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092"
	}

	kafkaWriter := &kafka.Writer{
		Addr:     kafka.TCP(kafkaBroker),
		Topic:    "click-events",
		Balancer: &kafka.LeastBytes{},
	}
	defer kafkaWriter.Close()

	// 2. Pass kafkaWriter into your URLHandler so it's not nil
	urlHandler = &handlers.URLHandler{
		DB:          pgPool,
		RedisClient: redisClient,
		KafkaWriter: kafkaWriter, // <-- This resolves the nil pointer panic
		URLSvc:      urlSvc,
	}
	
	log.Println("--> 🚀 Starting Kafka analytics consumer worker...")
	go func() {
    	if err := service.StartAnalyticsConsumer(kafkaBroker, pgPool); err != nil {
        	log.Printf("--> ❌ [CONSUMER ERROR] %v", err)
    	}
	}()

	// 5. Setup HTTP routes & Middleware
	mux := http.NewServeMux()
	
	authHandler := &handlers.AuthHandler{
		DB: pgPool,
	}

	// API Routes with Explicit HTTP Methods
	mux.HandleFunc("POST /api/v1/register", authHandler.Register)
	mux.HandleFunc("POST /api/v1/login", authHandler.Login)
	mux.HandleFunc("GET /api/v1/my-urls", middleware.AuthMiddleware(urlHandler.GetMyURLs))
	mux.HandleFunc("GET /api/user/urls", urlHandler.GetUserURLs)
	mux.HandleFunc("DELETE /api/v1/url/delete", middleware.AuthMiddleware(urlHandler.DeleteURL))
	mux.HandleFunc("GET /api/v1/analytics", middleware.AuthMiddleware(urlHandler.GetAnalytics))
	mux.HandleFunc("GET /dashboard.html", urlHandler.Dashboard)
	
	secureAndRateLimitedHandler := middleware.RateLimiter(redisClient, 20, 1*time.Minute)(
		middleware.OptionalAuthMiddleware(urlHandler.ShortenURL),
	)
	mux.HandleFunc("POST /api/v1/shorten", secureAndRateLimitedHandler)

	// Set up embedded static file sub-FS
	webFS, err := fs.Sub(contentEmbed, "web")
	if err != nil {
		log.Fatalf("Failed to create embedded file system: %v", err)
	}

	// Helper function to serve files safely without redirect loops
	serveFile := func(w http.ResponseWriter, r *http.Request, filename string) {
		file, err := webFS.Open(filename)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}

		http.ServeContent(w, r, stat.Name(), stat.ModTime(), file.(io.ReadSeeker))
	}

	// Explicit Page Routes with Methods (Using /{$} for precise root matching)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, "index.html")
	})
	mux.HandleFunc("GET /index.html", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, "index.html")
	})
	mux.HandleFunc("GET /login.html", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, "login.html")
	})

	// Short Code Redirection Route
	mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		if code == "" || code == "index.html" || code == "dashboard.html" || code == "login.html" ||
			strings.HasPrefix(code, "api") || strings.Contains(code, ".") {
			http.NotFound(w, r)
			return
		}
		urlHandler.Redirect(w, r)
	})

	// 6. Start Server
	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	
	log.Printf("Brevity production server running on port %s...", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
