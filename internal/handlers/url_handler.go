package handlers

import (
	"html/template"
	"time"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"context"
	"brevity/internal/service"
	"brevity/internal/models"
	"github.com/segmentio/kafka-go"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type URLHandler struct {
    DB          *pgxpool.Pool
    RedisClient *redis.Client
    KafkaWriter *kafka.Writer 
    URLSvc      *service.URLService
}

func NewURLHandler(urlSvc *service.URLService, db *pgxpool.Pool, rdb *redis.Client) *URLHandler {
	return &URLHandler{
		URLSvc:      urlSvc,
		DB:          db,
		RedisClient: rdb,
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

func parseDeviceType(ua string) string {
    uaLower := strings.ToLower(ua)
    if strings.Contains(uaLower, "mobile") || strings.Contains(uaLower, "android") || strings.Contains(uaLower, "iphone") {
        return "Mobile"
    } else if strings.Contains(uaLower, "ipad") || strings.Contains(uaLower, "tablet") {
        return "Tablet"
    }
    return "Desktop"
}

func (h *URLHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query := `
		SELECT u.short_code, c.clicked_at, c.user_agent, c.ip_address, c.referrer, c.country, c.device
		FROM click_events c
		JOIN urls u ON c.url_id = u.id
		ORDER BY c.clicked_at DESC
		LIMIT 50
	`
	rows, err := h.DB.Query(ctx, query)
	if err != nil {
		log.Printf("--> ❌ [DB ERROR] Failed to load dashboard analytics: %v", err)
		http.Error(w, "Failed to load analytics", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var clicks []models.ClickEvent
	for rows.Next() {
		var ce models.ClickEvent
		if err := rows.Scan(&ce.ShortCode, &ce.Timestamp, &ce.UserAgent, &ce.IPAddress, &ce.Referrer, &ce.Country, &ce.Device); err != nil {
			log.Printf("--> ⚠️ [SCAN ERROR] %v", err)
			continue
		}
		clicks = append(clicks, ce)
	}

	data := map[string]interface{}{
		"Clicks": clicks,
	}

	// Parse and execute the template directly
	tmpl, err := template.ParseFiles("web/dashboard.html")
	if err != nil {
		log.Printf("--> ❌ [TEMPLATE ERROR] %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("--> ❌ [TEMPLATE EXEC ERROR] %v", err)
	}
}

func (h *URLHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	shortCode := r.PathValue("code")
	if shortCode == "" {
		shortCode = strings.TrimPrefix(r.URL.Path, "/")
	}

	log.Printf("--> 🚀 [REDIRECT HIT] Incoming request for short code: '%s'", shortCode)

	if strings.Contains(shortCode, ".") || shortCode == "dashboard" || shortCode == "login" || shortCode == "" {
		log.Printf("--> [1.1] Ignoring static/reserved path: '%s'", shortCode)
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	cacheKey := "url:" + shortCode
	log.Printf("--> [2] Checking Redis cache for key: '%s'", cacheKey)

	// 2. Try Redis Cache
	cachedURL, err := h.RedisClient.Get(ctx, cacheKey).Result()
	if err == nil && cachedURL != "" {
		log.Printf("--> 🔥 [CACHE HIT] Found '%s' in Redis! Redirecting instantly to: '%s'", shortCode, cachedURL)

		// Asynchronously publish click event to Kafka on Cache Hit
		go func() {
			bgCtx := context.Background()
			referrer := r.Header.Get("Referer")
			if referrer == "" {
				referrer = "Direct"
			}

			country := r.Header.Get("CF-IPCountry")
			if country == "" {
				country = "BD"
			}

			device := parseDeviceType(r.UserAgent())

			event := models.ClickEvent{
				ShortCode: shortCode,
				Timestamp: time.Now(),
				UserAgent: r.UserAgent(),
				IPAddress: r.RemoteAddr,
				Referrer:  referrer,
				Country:   country,
				Device:    device,
			}

			payload, err := json.Marshal(event)
			if err != nil {
				log.Printf("--> ⚠️ [KAFKA] Failed to marshal click event: %v", err)
				return
			}

			err = h.KafkaWriter.WriteMessages(bgCtx, kafka.Message{
				Key:   []byte(shortCode),
				Value: payload,
			})
			if err != nil {
				log.Printf("--> ⚠️ [KAFKA] Failed to write click event: %v", err)
			}
		}()

		http.Redirect(w, r, cachedURL, http.StatusTemporaryRedirect)
		return
	}

	if err != nil {
		log.Printf("--> ❄️ [CACHE MISS] Redis get returned error (normal if first time): %v", err)
	}

	// 3. Query PostgreSQL for the full URL record (to check expiration reliably)
	log.Printf("--> [3] Querying PostgreSQL for short code: '%s'", shortCode)
	urlRecord, err := h.URLSvc.GetURLRecord(ctx, shortCode)
	if err != nil {
		log.Printf("--> ❌ [DB ERROR] Failed to find long URL for code '%s': %v", shortCode, err)
		http.NotFound(w, r)
		return
	}

	// Check if the link has expired
	if urlRecord.ExpiresAt != nil && time.Now().After(*urlRecord.ExpiresAt) {
		log.Printf("--> ⏰ [EXPIRED] Short link '%s' has expired (expired at %v)", shortCode, *urlRecord.ExpiresAt)
		http.Error(w, "This link has expired", http.StatusGone)
		return
	}

	originalURL := urlRecord.LongURL

	log.Printf("--> [4] Found in DB: '%s'. Writing back to Redis cache...", originalURL)

	// 4. Populate Redis Cache with proper TTL based on expiration if available
	ttl := 24 * time.Hour
	if urlRecord.ExpiresAt != nil {
		ttl = time.Until(*urlRecord.ExpiresAt)
		if ttl < 0 {
			ttl = time.Minute
		}
	}

	if setErr := h.RedisClient.Set(ctx, cacheKey, originalURL, ttl).Err(); setErr != nil {
		log.Printf("--> ⚠️ [REDIS SET ERROR] Failed to write to cache: %v", setErr)
	} else {
		log.Printf("--> ✅ [CACHE POPULATED] Successfully cached '%s' in Redis", cacheKey)
	}

	log.Printf("--> [5] Redirecting '%s' to '%s' via Database fallback", shortCode, originalURL)

	go func() {
		bgCtx := context.Background()

		referrer := r.Header.Get("Referer")
		if referrer == "" {
			referrer = "Direct"
		}

		// Get Country from proxy header (e.g., Cloudflare) or fallback
		country := r.Header.Get("CF-IPCountry")
		if country == "" {
			country = "BD"
		}

		device := parseDeviceType(r.UserAgent())

		event := models.ClickEvent{
			ShortCode: shortCode,
			Timestamp: time.Now(),
			UserAgent: r.UserAgent(),
			IPAddress: r.RemoteAddr,
			Referrer:  referrer,
			Country:   country,
			Device:    device,
		}

		payload, err := json.Marshal(event)
		if err != nil {
			log.Printf("--> ⚠️ [KAFKA] Failed to marshal click event: %v", err)
			return
		}

		err = h.KafkaWriter.WriteMessages(bgCtx, kafka.Message{
			Key:   []byte(shortCode),
			Value: payload,
		})
		if err != nil {
			log.Printf("--> ⚠️ [KAFKA] Failed to write click event: %v", err)
		}
	}()

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

	links, err := h.URLSvc.GetURLsByUserID(r.Context(), userID)
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

    if userIDPtr != nil {
        log.Printf("--> 🔍 [HANDLER] Extracted user ID: %d", *userIDPtr)
    } else {
        log.Printf("--> 🔍 [HANDLER] User is anonymous (userIDPtr is nil)")
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
	shortURL, err := h.URLSvc.CreateShortURLWithAlias(r.Context(), createReq.LongURL, createReq.CustomAlias, userIDPtr, finalExpiresAt)
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
func (h *URLHandler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
    shortCode := r.URL.Query().Get("code")
	log.Printf("--> [ANALYTICS] Request received for short code: '%s'", shortCode)
    if shortCode == "" {
        http.Error(w, "Missing short code parameter", http.StatusBadRequest)
        return
    }

    ctx := r.Context()

    // 1. Extract user ID from context (set by AuthMiddleware)
    //userID, ok := ctx.Value("userID").(int64)
    //if !ok {
    //    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    //    return
    //}
	rawUserID, ok := ctx.Value("userID").(float64)
	    if !ok {
			log.Printf("--> ❌ [ANALYTICS ERROR] Failed to extract 'userID' from context")
	        http.Error(w, "Unauthorized", http.StatusUnauthorized)
	        return
	    }
	userID := int64(rawUserID)
				
	    // 2. Verify that the URL exists AND belongs to the authenticated user
    var urlID int64
    err := h.DB.QueryRow(ctx, "SELECT id FROM urls WHERE short_code = $1 AND user_id = $2", shortCode, userID).Scan(&urlID)
    if err != nil {
		log.Printf("--> ❌ [ANALYTICS DB ERROR] URL not found or unowned for code '%s' and user %d: %v", shortCode, userID, err)
        log.Printf("--> 🔒 [UNAUTHORIZED ACCESS] User %d attempted to view analytics for unowned code '%s'", userID, shortCode)
        http.Error(w, "URL not found or unauthorized", http.StatusNotFound)
        return
    }

    // 3. Fetch total clicks using verified url_id
    var totalClicks int
    err = h.DB.QueryRow(ctx, `SELECT COUNT(*) FROM click_events WHERE url_id = $1`, urlID).Scan(&totalClicks)
    if err != nil {
        http.Error(w, "Error fetching analytics", http.StatusInternalServerError)
        return
    }

    // 4. Fetch breakdown by device
    deviceRows, err := h.DB.Query(ctx, `
        SELECT device, COUNT(*) as count 
        FROM click_events 
        WHERE url_id = $1 
        GROUP BY device 
        ORDER BY count DESC`, urlID)
    if err != nil {
        http.Error(w, "Error fetching device stats", http.StatusInternalServerError)
        return
    }
    defer deviceRows.Close()

    devices := make(map[string]int)
    for deviceRows.Next() {
        var dev string
        var count int
        if err := deviceRows.Scan(&dev, &count); err == nil {
            devices[dev] = count
        }
    }

    // 5. Construct response
    response := map[string]interface{}{
        "short_code":   shortCode,
        "total_clicks": totalClicks,
        "devices":      devices,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}
