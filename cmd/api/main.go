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

    // Set up embedded static file server sub-FS
    webFS, err := fs.Sub(contentEmbed, "web")
    if err != nil {
        log.Fatalf("Failed to create embedded file system: %v", err)
    }
    fileServer := http.FileServer(http.FS(webFS))

    // Apply Rate Limiter middleware to the shorten endpoint
    secureAndRateLimitedHandler := middleware.RateLimiter(redisClient, 20, 1*time.Minute)(
        middleware.OptionalAuthMiddleware(urlHandler.ShortenURL),
    )
    mux.HandleFunc("/api/v1/shorten", secureAndRateLimitedHandler)

    // Root handler: Serve static assets or index.html, fallback to 404
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        log.Printf("--> 🌐 ROOT ROUTER CAUGHT PATH: %s", r.URL.Path)
        path := r.URL.Path

        // Serve index.html for root path
        if path == "/" || path == "" {
            r.URL.Path = "/index.html"
            fileServer.ServeHTTP(w, r)
            return
        }

        // Check if the requested file exists in embedded web files (e.g. dashboard.html, login.html, css/js)
        cleanedPath := strings.TrimPrefix(path, "/")
        if _, err := fs.Stat(webFS, cleanedPath); err == nil {
            fileServer.ServeHTTP(w, r)
            return
        }

        // If not a static file, return 404 (do not redirect loop)
        http.NotFound(w, r)
    })

    // Short code redirect route (Go 1.22+ wildcard matching)
    mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
        code := r.PathValue("code")
        // Ignore reserved system or static file names if caught here
        if code == "" || code == "index.html" || code == "dashboard.html" || code == "login.html" || strings.HasPrefix(code, "api") {
            http.NotFound(w, r)
            return
        }
        urlHandler.Redirect(w, r)
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
