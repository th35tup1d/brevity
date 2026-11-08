package service

import (
	"context"
	"math/rand"
	"time"
	"log"
	"brevity/internal/models"
	"brevity/internal/repository"

	"github.com/redis/go-redis/v9"
)

const base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type URLService struct {
	repo        *repository.URLRepository
	redisClient *redis.Client
}

type URLRecord struct {
    LongURL   string
    ExpiresAt *time.Time
}

func NewURLService(repo *repository.URLRepository, redisClient *redis.Client) *URLService {
	return &URLService{
		repo:        repo,
		redisClient: redisClient,
	}
}

func (s *URLService) GetURLsByUserID(ctx context.Context, userID int64) ([]models.URL, error) {
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

// CreateShortURL handles standard random short code generation
func (s *URLService) CreateShortURL(ctx context.Context, longURL string, userID *int64) (*models.URL, error) {

	if userID != nil && *userID <= 0 {
		userID = nil
	}
	
	shortCode := generateShortCode()
	isCustom := false

	url, err := s.repo.InsertURL(ctx, longURL, shortCode, userID, isCustom)
	if err != nil {
		return nil, err
	}

	if s.redisClient != nil {
		_ = s.redisClient.Set(ctx, shortCode, longURL, 24*time.Hour).Err()
	}

	return url, nil
}

func (s *URLService) CreateShortURLWithAlias(ctx context.Context, longURL string, customAlias string, userID *int64, expiresAt *time.Time) (*models.URL, error) {
	var shortCode string
	var isCustom bool

	if customAlias != "" {
		shortCode = customAlias
		isCustom = true
	} else {
		shortCode = generateShortCode()
		isCustom = false
	}

	// If user is anonymous, override any custom expiration to 24 hours
	if userID == nil {
		defaultExp := time.Now().Add(24 * time.Hour)
		expiresAt = &defaultExp
	}

	// Pass the generated shortCode, isCustom, and expiresAt into your repository
	url, err := s.repo.CreateURL(ctx, userID, longURL, shortCode, isCustom, expiresAt)
	if err != nil {
		return nil, err
	}

	if s.redisClient != nil {
		// Calculate appropriate Redis TTL based on the expiration time if available
		ttl := 24 * time.Hour
		if expiresAt != nil {
			ttl = time.Until(*expiresAt)
			if ttl < 0 {
				ttl = time.Minute
			}
		}
		_ = s.redisClient.Set(ctx, shortCode, longURL, ttl).Err()
	}

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


func (s *URLService) GetURLRecord(ctx context.Context, shortCode string) (*models.URLRecord, error) {
    return s.repo.GetURLRecord(ctx, shortCode)
}

func (s *URLService) GetLongURL(ctx context.Context, shortCode string) (string, error) {
	cachedURL, err := s.redisClient.Get(ctx, shortCode).Result()
	if err == nil {
		return cachedURL, nil
	}

	longURL, err := s.repo.FindByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	_ = s.redisClient.Set(ctx, shortCode, longURL, 24*time.Hour).Err()

	return longURL, nil
}

func (s *URLService) StartCleanupWorker(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    go func() {
        for {
            select {
            case <-ticker.C:
                count, err := s.repo.DeleteExpiredURLs(ctx)
                if err != nil {
                    log.Printf("--> ⚠️ [CLEANUP ERROR] Failed to delete expired URLs: %v", err)
                } else if count > 0 {
                    log.Printf("--> 🧹 [CLEANUP] Successfully removed %d expired short links from database", count)
                }
            case <-ctx.Done():
                ticker.Stop()
                return
            }
        }
    }()
}

func (s *URLService) IncrementClicks(ctx context.Context, shortCode string) error {
	return s.repo.IncrementClicks(ctx, shortCode)
}
