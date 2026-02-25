package config

import (
	"encoding/json"
	"os"
	"strconv"
)

type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	DB   string `json:"db"`
}

func Load(path string) (*Config, error) {
	// Start with defaults
	cfg := &Config{
		Host: "127.0.0.1",
		Port: 8080,
		DB:   "llmgw.db",
	}

	// Try to load from file
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		var fileCfg Config
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&fileCfg); err == nil {
			// Override defaults with file values
			if fileCfg.Host != "" {
				cfg.Host = fileCfg.Host
			}
			if fileCfg.Port != 0 {
				cfg.Port = fileCfg.Port
			}
			if fileCfg.DB != "" {
				cfg.DB = fileCfg.DB
			}
		}
	}

	// Environment variables take precedence
	if host := os.Getenv("LLMGW_HOST"); host != "" {
		cfg.Host = host
	}
	if port := os.Getenv("LLMGW_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}
	if db := os.Getenv("LLMGW_DB"); db != "" {
		cfg.DB = db
	}

	return cfg, nil
}