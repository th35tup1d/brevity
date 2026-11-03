package repository

import (
	"context"
	"errors"
	"fmt"
	"brevity/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type URLRepository struct {
	db *pgxpool.Pool
}


func (r *URLRepository) InsertURL(ctx context.Context, longURL string, shortCode string, userID *int64) (*models.URL, error) {
    query := `INSERT INTO urls (long_url, short_code, user_id) VALUES ($1, $2, $3) RETURNING id, long_url, short_code, user_id, created_at`
    
    var url models.URL
    // PostgreSQL automatically handles userID as NULL if it's a nil *int64 pointer
    err := r.db.QueryRow(ctx, query, longURL, shortCode, userID).Scan(
        &url.ID, &url.OriginalURL, &url.ShortCode, &url.UserID, &url.CreatedAt,
    )
    if err != nil {
        return nil, err
    }
    return &url, nil
}


func NewURLRepository(db *pgxpool.Pool) *URLRepository {
	return &URLRepository{db: db}
}

// InsertURL inserts a long URL with a placeholder short code and returns the auto-generated ID
// UpdateShortCode updates the row with the calculated Base62 short code
func (r *URLRepository) UpdateShortCode(ctx context.Context, id int64, shortCode string) error {
	query := `UPDATE urls SET short_code = $1 WHERE id = $2`
	_, err := r.db.Exec(ctx, query, shortCode, id)
	if err != nil {
		return fmt.Errorf("failed to update short code: %w", err)
	}
	return nil
}

func (r *URLRepository) GetURLsByUserID(ctx context.Context, userID int64) ([]models.URL, error) {
	query := "SELECT id, original_url, short_code, created_at, user_id FROM urls WHERE user_id = $1 ORDER BY id DESC"
    rows, err := r.db.Query(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var urls []models.URL
    for rows.Next() {
        var u models.URL
        if err := rows.Scan(&u.ID, &u.OriginalURL, &u.ShortCode, &u.CreatedAt, &u.UserID); err != nil {
            return nil, err
        }
        urls = append(urls, u)
    }
    return urls, nil
}

// FindByShortCode retrieves the original long URL given a short code
func (r *URLRepository) FindByShortCode(ctx context.Context, shortCode string) (string, error) {
	query := `SELECT long_url FROM urls WHERE short_code = $1`
	var longURL string
	err := r.db.QueryRow(ctx, query, shortCode).Scan(&longURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("short code not found")
		}
		return "", fmt.Errorf("failed to query short code: %w", err)
	}
	return longURL, nil
}

// RecordClick inserts an analytics click event for a given URL ID
func (r *URLRepository) RecordClick(ctx context.Context, urlID int64, ipAddress, userAgent string) error {
	query := `INSERT INTO click_events (url_id, ip_address, user_agent) VALUES ($1, $2, $3)`
	_, err := r.db.Exec(ctx, query, urlID, ipAddress, userAgent)
	return err
}

// GetURLIDByShortCode fetches just the primary key ID for analytics logging
func (r *URLRepository) GetURLIDByShortCode(ctx context.Context, shortCode string) (int64, error) {
	query := `SELECT id FROM urls WHERE short_code = $1`
	var id int64
	err := r.db.QueryRow(ctx, query, shortCode).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
