package models

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDatabase(dsn string) error {
	var err error
	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return err
	}

	// Auto migrate
	err = DB.AutoMigrate(
		&User{},
		&Admin{},
		&APIKey{},
		&UpstreamConfig{},
		&Model{},
		&ModelUpstream{},
		&UsageLog{},
	)
	if err != nil {
		return err
	}

	return nil
}