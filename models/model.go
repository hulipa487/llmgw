package models

import (
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID              uint             `gorm:"primaryKey" json:"id"`
	Name            string           `gorm:"uniqueIndex;size:255;not null" json:"name"` // Model name exposed at gateway endpoint
	PriceInputPerM  float64          `gorm:"default:0" json:"price_input_per_m"`
	PriceOutputPerM float64          `gorm:"default:0" json:"price_output_per_m"`
	IsEnabled       bool             `gorm:"default:true;index" json:"is_enabled"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	DeletedAt       gorm.DeletedAt   `gorm:"index" json:"-"`
	Upstreams       []UpstreamConfig `gorm:"many2many:model_upstreams;joinForeignKey:ModelID;joinReferencesForeignKey:UpstreamConfigID" json:"upstreams,omitempty"`
}

func (Model) TableName() string {
	return "models"
}

// ModelUpstream is the junction table with additional fields for per-upstream model name
type ModelUpstream struct {
	ModelID           uint      `gorm:"primaryKey;autoIncrement:false" json:"model_id"`
	UpstreamConfigID   uint      `gorm:"primaryKey;autoIncrement:false" json:"upstream_config_id"`
	UpstreamModelName  string    `gorm:"size:255;not null" json:"upstream_model_name"` // Model name to send to upstream
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (ModelUpstream) TableName() string {
	return "model_upstreams"
}