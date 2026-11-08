package models

import "time"

type ClickEvent struct {
    ShortCode string    `json:"short_code"`
    Timestamp time.Time `json:"timestamp"`
    UserAgent string    `json:"user_agent"`
    IPAddress string    `json:"ip_address"`
    Referrer  string    `json:"referrer"`
    Country   string    `json:"country"`
    Device    string    `json:"device"`
}
