package handlers

import (
	"encoding/json"
	"os"
	"net/http"
	"strconv"
	"time"
	"strings"
	"github.com/jackc/pgx/v5/pgxpool"
	"brevity/internal/service"
	"log"
	"github.com/redis/go-redis/v9"
)

//type URLHandler struct {
//	DB *pgxpool.Pool
//}

func (h *URLHandler) GetMyURLs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawUserID := r.Context().Value("userID")
	if rawUserID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userID int
	switch v := rawUserID.(type) {
	case float64:
		userID = int(v)
	case int:
		userID = v
	default:
		http.Error(w, "Invalid user ID type", http.StatusInternalServerError)
		return
	}

	rows, err := h.DB.Query(r.Context(), "SELECT id, long_url, short_code, created_at FROM urls WHERE user_id = $1 ORDER BY id DESC", userID)
	if err != nil {
		http.Error(w, "DB Error: "+err.Error(), http.StatusInternalServerError)
		//http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type LinkResponse struct {
		ID          int    `json:"id"`
		OriginalURL string `json:"original_url"`
		ShortCode   string `json:"short_code"`
		CreatedAt   string `json:"created_at"`
	}

	var links []LinkResponse
	for rows.Next() {
		var l LinkResponse
		var createdAt time.Time
		if err := rows.Scan(&l.ID, &l.OriginalURL, &l.ShortCode, &createdAt); err != nil {
			continue
		}
		l.CreatedAt = createdAt.Format("2006-01-02 15:04")
		links = append(links, l)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

// Define the struct fields to match what you are assigning
type URLHandler struct {
    urlSvc        *service.URLService
    DB *pgxpool.Pool
	redisClient *redis.Client
}



func NewURLHandler(urlSvc *service.URLService, db *pgxpool.Pool, rdb *redis.Client) *URLHandler {
    return &URLHandler{
        urlSvc:      urlSvc,
        DB:          db,
        redisClient: rdb,
    }
}


func (h *URLHandler) DeleteURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawUserID := r.Context().Value("userID")
	if rawUserID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userID int
	switch v := rawUserID.(type) {
	case float64:
		userID = int(v)
	case int:
		userID = v
	default:
		http.Error(w, "Invalid user ID type", http.StatusInternalServerError)
		return
	}

	// Get URL ID from query parameter (?id=123)
	idStr := r.URL.Query().Get("id")
	urlID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid link ID", http.StatusBadRequest)
		return
	}

	// Delete only if the link belongs to this user
	cmdTag, err := h.DB.Exec(r.Context(), "DELETE FROM urls WHERE id = $1 AND user_id = $2", urlID, userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if cmdTag.RowsAffected() == 0 {
		http.Error(w, "Link not found or unauthorized", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

func (h *URLHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	shortCode := r.PathValue("code")
	if shortCode == "" {
		shortCode = strings.TrimPrefix(r.URL.Path, "/")
	}
	
	if strings.Contains(shortCode, ".") || shortCode == "dashboard" || shortCode == "login" || shortCode == "" {
		log.Printf("--> [1.1] Ignoring static/reserved path: '%s'", shortCode)
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	cacheKey := "url:" + shortCode
	log.Printf("--> [2] Checking Redis cache for key: '%s'", cacheKey)

	// 2. Try Redis Cache
	cachedURL, err := h.redisClient.Get(ctx, cacheKey).Result()
	if err == nil && cachedURL != "" {
		log.Printf("--> 🔥 [CACHE HIT] Found '%s' in Redis! Redirecting instantly to: '%s'", shortCode, cachedURL)
		http.Redirect(w, r, cachedURL, http.StatusTemporaryRedirect)
		return
	}

	if err != nil {
		log.Printf("--> ❄️ [CACHE MISS] Redis get returned error (normal if first time): %v", err)
	}

	// 3. Fall back to Database
	log.Printf("--> [3] Querying PostgreSQL for short code: '%s'", shortCode)
	originalURL, err := h.urlSvc.GetLongURL(ctx, shortCode)
	if err != nil {
		log.Printf("--> ❌ [DB ERROR] Failed to find long URL for code '%s': %v", shortCode, err)
		http.Error(w, "Short link not found", http.StatusNotFound)
		return
	}   

	log.Printf("--> [4] Found in DB: '%s'. Writing back to Redis cache...", originalURL)

	// 4. Populate Redis Cache
	if setErr := h.redisClient.Set(ctx, cacheKey, originalURL, 24*time.Hour).Err(); setErr != nil {
		log.Printf("--> ⚠️ [REDIS SET ERROR] Failed to write to cache: %v", setErr)
	} else {
		log.Printf("--> ✅ [CACHE POPULATED] Successfully cached '%s' in Redis", cacheKey)
	}

	log.Printf("--> [5] Redirecting '%s' to '%s' via Database fallback", shortCode, originalURL)
	http.Redirect(w, r, originalURL, http.StatusTemporaryRedirect)
}


func (h *URLHandler) GetUserURLs(w http.ResponseWriter, r *http.Request) {
    // Safely extract user_id, handling potential type variations from JWT claims
    val := r.Context().Value("user_id")
    if val == nil {
        http.Error(w, "Unauthorized: no user context found", http.StatusUnauthorized)
        return
    }

    var userID int64
    switch v := val.(type) {
    case int64:
        userID = v
    case int:
        userID = int64(v)
    case float64:
        userID = int64(v)
    default:
        http.Error(w, "Unauthorized: invalid user ID type", http.StatusUnauthorized)
        return
    }

    // Fetch links from service
    links, err := h.urlSvc.GetURLsByUserID(r.Context(), userID)
    if err != nil {
		http.Error(w, "DEBUG_TEST_NEW_CODE_IS_RUNNING", http.StatusInternalServerError)
		//http.Error(w, err.Error(), http.StatusInternalServerError)
        // This writes the exact database error straight to the HTTP response
        //http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(links)
}

func (h *URLHandler) ShortenURL(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // 1. Decode the request body once
    var createReq struct {
        LongURL string `json:"url"`
    }
    if err := json.NewDecoder(r.Body).Decode(&createReq); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    // 2. Safely extract the user ID from the context (supporting *int64, int64, or float64 formats)
    var userIDPtr *int64
    if rawUserID := r.Context().Value("user_id"); rawUserID != nil {
        switch id := rawUserID.(type) {
        case *int64:
            userIDPtr = id
        case int64:
            userIDPtr = &id
        case float64:
            val := int64(id)
            userIDPtr = &val
        }
    } else if rawAlt := r.Context().Value("userID"); rawAlt != nil {
        // Fallback for alternate context key naming conventions
        if floatID, ok := rawAlt.(float64); ok {
            val := int64(floatID)
            userIDPtr = &val
        }
    }

    // 3. Delegate to the service layer (handles code generation, DB insert, and optional Redis caching)
    shortURL, err := h.urlSvc.CreateShortURL(r.Context(), createReq.LongURL, userIDPtr)
    if err != nil {
        log.Println("ERROR in CreateShortURL service:", err)
        http.Error(w, "Database error while saving URL", http.StatusInternalServerError)
        return
    }

    // 4. Send the successful JSON response back to the client cleanly
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
	    baseURL = "http://localhost:8080"
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
	    "short_url": baseURL + "/" + shortURL.ShortCode,
	})
}
