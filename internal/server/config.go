package server

import (
	"os"
	"strconv"
)

// Config holds server configuration.
type Config struct {
	Host      string
	Port      int
	DBPath    string
	DataDir   string
	JWTSecret string
	LogLevel  string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() *Config {
	cfg := &Config{
		Host:      getEnv("LARK_HOST", "0.0.0.0"),
		Port:      getEnvInt("LARK_PORT", 4001),
		DBPath:    getEnv("LARK_DB_PATH", "data/lark.db"),
		DataDir:   getEnv("LARK_DATA_DIR", "data"),
		JWTSecret: getEnv("LARK_JWT_SECRET", "change-me-in-production"),
		LogLevel:  getEnv("LARK_LOG_LEVEL", "info"),
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
