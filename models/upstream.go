package models

import (
	"time"
)

type UpstreamConfig struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	UpstreamID    string    `gorm:"uniqueIndex;size:255;not null" json:"upstream_id"`
	Name          string    `gorm:"size:255;not null" json:"name"`
	BaseURL       string    `gorm:"size:512;not null" json:"base_url"`
	OpenAIPath    string    `gorm:"size:255;not null;default:'/v1'" json:"openai_path"`
	AnthropicPath string    `gorm:"size:255;not null;default:'/v1'" json:"anthropic_path"`
	Key           string    `gorm:"size:255;not null" json:"key"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Models        []Model   `gorm:"many2many:model_upstreams;" json:"models,omitempty"`
}

func (UpstreamConfig) TableName() string {
	return "upstream_configs"
}