package server

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds server configuration.
type Config struct {
	Host       string
	Port       int
	DBPath     string
	DataDir    string
	JWTSecret  string
	LogLevel   string
	CORSOrigin string
	TLSCert    string
	TLSKey     string
	RateLimit  int
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() *Config {
	cfg := &Config{
		Host:       getEnv("LARK_HOST", "127.0.0.1"),
		Port:       getEnvInt("LARK_PORT", 4001),
		DBPath:     getEnv("LARK_DB_PATH", "data/lark.db"),
		DataDir:    getEnv("LARK_DATA_DIR", "data"),
		JWTSecret:  getEnv("LARK_JWT_SECRET", ""),
		LogLevel:   getEnv("LARK_LOG_LEVEL", "info"),
		CORSOrigin: getEnv("LARK_CORS_ORIGIN", ""),
		TLSCert:    getEnv("LARK_TLS_CERT", ""),
		TLSKey:     getEnv("LARK_TLS_KEY", ""),
		RateLimit:  getEnvInt("LARK_RATE_LIMIT", 100),
	}
	return cfg
}

// Validate checks that required config values are set.
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("LARK_JWT_SECRET is required (generate with: openssl rand -hex 32)")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("LARK_PORT must be between 1 and 65535")
	}
	return nil
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
