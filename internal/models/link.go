package models

import (
	"errors"
	"strings"
)


var reservedKeywords = map[string]bool{
	"api":      true,
	"admin":    true,
	"login":    true,
	"register": true,
	"dashboard": true,
	"health":   true,
	"static":   true,
}

type CreateLinkRequest struct {
	LongURL     string `json:"long_url"`
	CustomAlias string `json:"custom_alias,omitempty"`
}

func ValidateAlias(alias string) error {
	if alias == "" {
		return nil // Optional, falls back to auto-generation
	}
	
	// Normalize to lowercase if your app treats aliases case-insensitively
	alias = strings.ToLower(alias)

	if reservedKeywords[alias] {
		return errors.New("this alias is reserved and cannot be used")
	}

	return nil
}
