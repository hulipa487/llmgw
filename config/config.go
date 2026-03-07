package config

import (
	"encoding/json"
	"os"
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
		DB:   "",
	}

	// Try to load from file
	file, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer file.Close()

	var fileCfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&fileCfg); err != nil {
		return cfg, err
	}

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

	return cfg, nil
}