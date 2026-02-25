package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Username     string         `gorm:"uniqueIndex;size:255;not null" json:"username"`
	PasswordHash string         `gorm:"size:255;not null" json:"-"`
	Email        string         `gorm:"size:255" json:"email"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	APIKeys      []APIKey       `gorm:"foreignKey:UserID" json:"api_keys,omitempty"`
}

func (User) TableName() string {
	return "users"
}

type Admin struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Username     string         `gorm:"uniqueIndex;size:255;not null" json:"username"`
	PasswordHash string         `gorm:"size:255;not null" json:"-"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Admin) TableName() string {
	return "admins"
}