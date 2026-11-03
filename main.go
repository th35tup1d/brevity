package main

import (
    "embed"
    "io"
    "io/fs"
    "log"
    "net/http"
    "path"
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

    // 3. Initialize Redis client
    redisClient, err := database.NewRedisClient(cfg.RedisAddr)
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

    // 5. Setup HTTP routes & Middleware
    mux := http.NewServeMux()
    
    authHandler := &handlers.AuthHandler{
        DB: pgPool,
    }

    // API Routes
    mux.HandleFunc("/api/v1/register", authHandler.Register)
    mux.HandleFunc("/api/v1/login", authHandler.Login)
    mux.HandleFunc("/api/v1/my-urls", middleware.AuthMiddleware(urlHandler.GetMyURLs))
    mux.HandleFunc("GET /api/user/urls", urlHandler.GetUserURLs)
    mux.HandleFunc("/api/v1/url/delete", middleware.AuthMiddleware(urlHandler.DeleteURL))

    secureAndRateLimitedHandler := middleware.RateLimiter(redisClient, 20, 1*time.Minute)(
        middleware.OptionalAuthMiddleware(urlHandler.ShortenURL),
    )
    mux.HandleFunc("/api/v1/shorten", secureAndRateLimitedHandler)

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

    // Explicit Page Routes
    mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
        cleanPath := path.Clean(r.URL.Path)
        if cleanPath == "/" || cleanPath == "." || cleanPath == "" {
            serveFile(w, r, "index.html")
            return
        }

        // Strip leading slash for asset lookup
        assetName := strings.TrimPrefix(cleanPath, "/")

        // Check if asset exists in webFS
        if _, err := fs.Stat(webFS, assetName); err == nil {
            serveFile(w, r, assetName)
            return
        }

        // If it has a file extension and wasn't found, return 404 (no redirect)
        if strings.Contains(assetName, ".") {
            http.NotFound(w, r)
            return
        }

        http.NotFound(w, r)
    })

    mux.HandleFunc("GET /index.html", func(w http.ResponseWriter, r *http.Request) {
        serveFile(w, r, "index.html")
    })
    mux.HandleFunc("GET /dashboard.html", func(w http.ResponseWriter, r *http.Request) {
        serveFile(w, r, "dashboard.html")
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
