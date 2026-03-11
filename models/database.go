package models

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDatabase(dsn string) error {
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return err
	}

	// Auto migrate
	err = DB.AutoMigrate(
		&User{},
		&APIKey{},
		&UpstreamConfig{},
		&Model{},
		&ModelUpstream{},
		&UsageLog{},
		&APIKeyModelUpstream{},
	)
	if err != nil {
		return err
	}

	return nil
}
