package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"brevity/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Define the struct fields to match what you are assigning
type URLHandler struct {
	urlSvc      *service.URLService
	DB          *pgxpool.Pool
	redisClient *redis.Client
}

func NewURLHandler(urlSvc *service.URLService, db *pgxpool.Pool, rdb *redis.Client) *URLHandler {
	return &URLHandler{
		urlSvc:      urlSvc,
		DB:          db,
		redisClient: rdb,
	}
}

// Custom Alias Validation Helpers

var reservedKeywords = map[string]bool{
	"api":       true,
	"admin":     true,
	"login":     true,
	"register":  true,
	"dashboard": true,
	"health":    true,
	"static":    true,
}

func ValidateAlias(alias string) error {
	if alias == "" {
		return nil // Optional, falls back to auto-generation
	}

	alias = strings.ToLower(alias)

	if reservedKeywords[alias] {
		return errors.New("this alias is reserved and cannot be used")
	}

	return nil
}

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
	urlRecord, err := h.urlSvc.GetURLRecord(ctx, shortCode)
	if err != nil {
		log.Printf("--> ❌ [DB ERROR] Failed to find long URL for code '%s': %v", shortCode, err)
		http.Error(w, "Short link not found", http.StatusNotFound)
		return
	}    

	// Check if the link has expired
	if urlRecord.ExpiresAt != nil && time.Now().After(*urlRecord.ExpiresAt) {
		log.Printf("--> ⏰ [EXPIRED] Short link '%s' has expired", shortCode)
		http.Error(w, "This link has expired", http.StatusGone)
		return
	}

	originalURL := urlRecord.LongURL

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

	links, err := h.urlSvc.GetURLsByUserID(r.Context(), userID)
	if err != nil {
		http.Error(w, "DEBUG_TEST_NEW_CODE_IS_RUNNING", http.StatusInternalServerError)
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

	// 1. Decode the request body supporting url, optional custom_alias, and optional expires_at
	var createReq struct {
		LongURL     string     `json:"url"`
		CustomAlias string     `json:"custom_alias,omitempty"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&createReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 2. Validate custom alias if provided
	createReq.CustomAlias = strings.TrimSpace(createReq.CustomAlias)
	if err := ValidateAlias(createReq.CustomAlias); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 3. Safely extract the user ID from the context
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
		if floatID, ok := rawAlt.(float64); ok {
			val := int64(floatID)
			userIDPtr = &val
		}
	}

	// 4. Enforce that custom aliases are restricted to signed-in users only
	if createReq.CustomAlias != "" && userIDPtr == nil {
		http.Error(w, "Custom aliases are only available for signed-in users", http.StatusUnauthorized)
		return
	}

	// 5. Enforce expiration rules based on authentication status
	var finalExpiresAt *time.Time
	if userIDPtr == nil {
		// Anonymous users: Strictly enforce a 24-hour expiration
		defaultExpiry := time.Now().Add(24 * time.Hour)
		finalExpiresAt = &defaultExpiry
	} else {
		// Authenticated users: Allow requested custom expiration (can be nil for permanent)
		finalExpiresAt = createReq.ExpiresAt
	}

	// 6. Delegate to the service layer (passing finalExpiresAt)
	shortURL, err := h.urlSvc.CreateShortURLWithAlias(r.Context(), createReq.LongURL, createReq.CustomAlias, userIDPtr, finalExpiresAt)
	if err != nil {
		log.Println("ERROR in CreateShortURL service:", err)
		if err.Error() == "alias is already taken" {
			http.Error(w, "The custom alias is already taken. Please choose another.", http.StatusConflict)
			return
		}
		http.Error(w, "Database error while saving URL", http.StatusInternalServerError)
		return
	}

	// 7. Determine the message based on user status and expiration
	message := "Link created successfully"
	if userIDPtr == nil {
		message = "Link will expire in 24 hours"
	} else if createReq.ExpiresAt != nil {
		message = "Link created with custom expiration"
	}

	// 8. Send the successful JSON response back to the client cleanly
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"short_url":  baseURL + "/" + shortURL.ShortCode,
		"is_custom":  shortURL.IsCustom,
		"expires_at": finalExpiresAt,
		"message":    message,
	})
}
