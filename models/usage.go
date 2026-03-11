package models

import (
	"time"
)

type UsageLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	APIKeyID     uint      `gorm:"not null;index" json:"api_key_id"`
	UserID       int64     `gorm:"not null;index" json:"user_id"` // Telegram ID
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

// APIKeyModelUpstream stores sticky upstream assignment per API key + model
type APIKeyModelUpstream struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	APIKeyID        uint      `gorm:"not null;uniqueIndex:idx_apikey_model" json:"api_key_id"`
	ModelID         uint      `gorm:"not null;uniqueIndex:idx_apikey_model" json:"model_id"`
	UpstreamConfigID uint     `gorm:"not null" json:"upstream_config_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (APIKeyModelUpstream) TableName() string {
	return "api_key_model_upstreams"
}

// MaxAPIKeysPerUser is the maximum number of active API keys per user
const MaxAPIKeysPerUser = 10