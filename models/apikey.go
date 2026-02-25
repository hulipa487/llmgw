package models

import (
	"time"

	"gorm.io/gorm"
)

type APIKey struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	KeyHash     string         `gorm:"uniqueIndex;size:255;not null" json:"-"`
	UserID      uint           `gorm:"not null;index" json:"user_id"`
	User        User           `gorm:"foreignKey:UserID" json:"-"`
	Name        string         `gorm:"size:255" json:"name"`
	KeyPrefix   string         `gorm:"size:8" json:"key_prefix"` // First few chars for display
	CreatedAt   time.Time      `json:"created_at"`
	LastUsedAt  *time.Time     `json:"last_used_at"`
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (APIKey) TableName() string {
	return "api_keys"
}