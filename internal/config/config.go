// Package config loads runtime configuration from environment variables.
package config

import "os"

// Config holds all runtime settings. Every field has a safe local default
// except the LLM key, whose absence simply disables LLM extraction.
type Config struct {
	Addr        string
	DatabaseURL string
	RedisURL    string
	LLMAPIKey   string
	LLMBaseURL  string
	LLMModel    string
}

// Load reads configuration from the environment.
func Load() Config {
	return Config{
		Addr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://disaster:disaster@localhost:5432/disaster?sslmode=disable"),
		RedisURL:    getenv("REDIS_URL", "redis://localhost:6379/0"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMBaseURL:  getenv("LLM_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		LLMModel:    getenv("LLM_MODEL", "gemini-2.5-flash"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
