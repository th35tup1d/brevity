package models

import (
    "time"
)

type URL struct {
    ID          string    `json:"id" redis:"id"`
    OriginalURL string    `json:"original_url" redis:"original_url"`
    ShortCode   string    `json:"short_code" redis:"short_code"`
    UserID      *int64    `json:"user_id" redis:"user_id"`
    CreatedAt   time.Time `json:"created_at" redis:"created_at"`
	IsCustom    bool      `json:"is_custom"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type URLRecord struct {
    LongURL   string
    ExpiresAt *time.Time
}
