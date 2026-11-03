package main

import (
    "embed"
    "io/fs"
    "log"
    "net/http"
    "strings"
    "time"
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

    // 3. Initialize Redis client (Single source of truth, secure with password & ping check)
    redisClient, err := database.NewRedisClient(cfg.RedisAddr)
    if err != nil {
        log.Fatalf("Redis connection error: %v", err)
    }
    defer redisClient.Close()

    // 4. Initialize Repositories & Services
    urlRepo := repository.NewURLRepository(pgPool)
    
    // 5. Initialize Asynchronous Analytics Worker Pool (Buffer: 10000, 5 Workers)
    analyticsPool := service.NewAnalyticsWorkerPool(urlRepo, 10000, 5)
    defer analyticsPool.Stop() // Ensures graceful drain on shutdown

    urlSvc := service.NewURLService(urlRepo, redisClient)
    
    urlHandler := handlers.NewURLHandler(urlSvc, pgPool, redisClient)

    // 6. Setup HTTP routes & Middleware
    mux := http.NewServeMux()
    
    authHandler := &handlers.AuthHandler{
        DB: pgPool,
    }

    mux.HandleFunc("/api/v1/register", authHandler.Register)
    mux.HandleFunc("/api/v1/login", authHandler.Login)
    mux.HandleFunc("/api/v1/my-urls", middleware.AuthMiddleware(urlHandler.GetMyURLs))
    mux.HandleFunc("GET /api/user/urls", urlHandler.GetUserURLs)
    mux.HandleFunc("/api/v1/url/delete", middleware.AuthMiddleware(urlHandler.DeleteURL))

    // Set up embedded static file server
    webFS, err := fs.Sub(contentEmbed, "web")
    if err != nil {
        log.Fatalf("Failed to create embedded file system: %v", err)
    }
    fileServer := http.FileServer(http.FS(webFS))

    // Serve HTML pages explicitly from embedded files
    mux.HandleFunc("GET /dashboard.html", func(w http.ResponseWriter, r *http.Request) {
        r.URL.Path = "/dashboard.html"
        fileServer.ServeHTTP(w, r)
    })
    mux.HandleFunc("GET /index.html", func(w http.ResponseWriter, r *http.Request) {
        r.URL.Path = "/index.html"
        fileServer.ServeHTTP(w, r)
    })
    mux.HandleFunc("GET /login.html", func(w http.ResponseWriter, r *http.Request) {
        r.URL.Path = "/login.html"
        fileServer.ServeHTTP(w, r)
    })

    // Apply Rate Limiter middleware to the shorten endpoint
    secureAndRateLimitedHandler := middleware.RateLimiter(redisClient, 20, 1*time.Minute)(
        middleware.OptionalAuthMiddleware(urlHandler.ShortenURL),
    )
    mux.HandleFunc("/api/v1/shorten", secureAndRateLimitedHandler)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	    log.Printf("--> 🌐 ROOT ROUTER CAUGHT PATH: %s", r.URL.Path)
	    path := r.URL.Path
	
	    // Serve index.html for root path
	    if path == "/" || path == "" {
	        r.URL.Path = "/index.html"
	        fileServer.ServeHTTP(w, r)
	        return
	    }
	
	    // Check if the requested file exists in embedded web files
	    cleanedPath := strings.TrimPrefix(path, "/")
	    if _, err := fs.Stat(webFS, cleanedPath); err == nil {
	        fileServer.ServeHTTP(w, r)
	        return
	    }
	
	    // If it's not a static asset, check if it's a short code or return 404 
	    // to prevent infinite redirection loops on missing files/paths.
	    // If you want short codes handled here, delegate ONLY valid short code formats, 
	    // otherwise return http.NotFound:
	    http.NotFound(w, r)
		// Fallback to URL redirection handler for short codes
		// urlHandler.Redirect(w, r)

	})
	
    // 7. Start the HTTP API Server
    port := cfg.Port
    if port == "" {
        port = "8080"
    }
    
    log.Printf("Brevity production server running on port %s...", port)
    if err := http.ListenAndServe(":"+port, mux); err != nil {
        log.Fatalf("Server failed to start: %v", err)
    }
}
