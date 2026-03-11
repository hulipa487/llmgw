package models

import (
	"time"
)

type User struct {
	ID        int64     `gorm:"primaryKey" json:"id"` // Telegram ID from MTFPass
	Username  string    `gorm:"size:255" json:"username"`
	Role      string    `gorm:"size:50;default:user" json:"role"` // "user" or "admin"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	APIKeys   []APIKey  `gorm:"foreignKey:UserID" json:"api_keys,omitempty"`
}

func (User) TableName() string {
	return "users"
}

// IsAdmin checks if the user has admin role
func (u *User) IsAdmin() bool {
	return u.Role == "admin"
}
