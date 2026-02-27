package models

import (
	"time"
)

type UsageLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	APIKeyID     uint      `gorm:"not null;index" json:"api_key_id"`
	UserID       uint      `gorm:"not null;index" json:"user_id"`
	ModelName    string    `gorm:"size:255;not null;index" json:"model_name"`
	InputTokens  int       `gorm:"not null" json:"input_tokens"`
	OutputTokens int       `gorm:"not null" json:"output_tokens"`
	LatencyMs    int64     `gorm:"not null" json:"latency_ms"`
	CostUSD      float64   `gorm:"not null" json:"cost_usd"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

func (UsageLog) TableName() string {
	return "usage_logs"
}

// Rate limit constants
const (
	RateLimitPerWindow  = 800   // requests per 6 hours
	RateLimitPerMonth   = 9600  // requests per calendar month
	MaxAPIKeysPerUser   = 10    // max active API keys per user
)