package service

import (
	"context"
	"time"
	"math/rand"
	"brevity/internal/models"
	"brevity/internal/repository"
	"github.com/redis/go-redis/v9"
)

const base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type URLService struct {
	repo        *repository.URLRepository
	redisClient *redis.Client
}

func NewURLService(repo *repository.URLRepository, redisClient *redis.Client) *URLService {
	return &URLService{
		repo:        repo,
		redisClient: redisClient,
	}
}

func (s *URLService) GetURLsByUserID(ctx context.Context, userID int64) ([]models.URL, error) {
    // 1. Write the logic to query Redis or your database for the user's URLs
    // 2. Return the slice of URLs and any error encountered
	return s.repo.GetURLsByUserID(ctx, userID)    
}

// toBase62 converts a sequential database ID into a compact Base62 string
func toBase62(id int64) string {
	if id == 0 {
		return string(base62Chars[0])
	}
	var encoded []byte
	for id > 0 {
		remainder := id % 62
		encoded = append([]byte{base62Chars[remainder]}, encoded...)
		id /= 62
	}
	return string(encoded)
}

// internal/service/url_service.go

// Change the parameter from string to *int64
func (s *URLService) CreateShortURL(ctx context.Context, longURL string, userID *int64) (*models.URL, error) {
    // 1. Generate the random 6-character short code
    shortCode := generateShortCode()

    // 2. Insert into the database via repository
    url, err := s.repo.InsertURL(ctx, longURL, shortCode, userID)
    if err != nil {
        return nil, err
    }

    // 3. Optional: Cache the mapping in Redis for rapid future access (24-hour TTL)
    if s.redisClient != nil {
        _ = s.redisClient.Set(ctx, shortCode, longURL, 24*time.Hour).Err()
    }

    // 4. Return the full URL model
    return url, nil
}


func generateShortCode() string {
    const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
    b := make([]byte, 6)
    for i := range b {
        b[i] = charset[seededRand.Intn(len(charset))]
    }
    return string(b)
}
// GetLongURL implements the Cache-Aside pattern for blazing-fast redirects
func (s *URLService) GetLongURL(ctx context.Context, shortCode string) (string, error) {
	// 1. Check Redis cache first (Sub-millisecond lookup)
	cachedURL, err := s.redisClient.Get(ctx, shortCode).Result()
	if err == nil {
		return cachedURL, nil // Cache hit!
	}

	// 2. Cache miss: Fall back to querying PostgreSQL
	longURL, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	// 3. Populate Redis cache for subsequent requests
	_ = s.redisClient.Set(ctx, shortCode, longURL, 24*time.Hour).Err()

	return longURL, nil
}
