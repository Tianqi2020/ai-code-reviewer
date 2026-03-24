package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// Server
	Port string

	// GitHub
	GitHubToken         string
	GitHubWebhookSecret string // only required in server mode

	// OpenAI
	OpenAIAPIKey string
	OpenAIModel  string

	// Review behaviour
	MaxDiffLines   int      // truncate diffs larger than this
	ReviewLanguage string   // language for review comments (e.g. "English", "Chinese")
	IgnorePatterns []string
}

// Load reads configuration from environment variables and validates fields
// required by both the CLI review command and the webhook server.
// GITHUB_WEBHOOK_SECRET is optional here; server mode validates it separately.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		GitHubToken:         os.Getenv("GITHUB_TOKEN"),
		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		OpenAIAPIKey:        os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:         getEnv("OPENAI_MODEL", "gpt-4o"),
		ReviewLanguage:      getEnv("REVIEW_LANGUAGE", "English"),
		MaxDiffLines:        getEnvInt("MAX_DIFF_LINES", 2000),
		IgnorePatterns: getEnvSlice("IGNORE_PATTERNS", []string{
			"*.lock", "*.sum", "vendor/", "node_modules/", "*.pb.go",
		}),
	}

	var errs []string
	if cfg.GitHubToken == "" {
		errs = append(errs, "GITHUB_TOKEN is required")
	}
	if cfg.OpenAIAPIKey == "" {
		errs = append(errs, "OPENAI_API_KEY is required")
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}

	return cfg, nil
}

// ValidateServerMode checks the additional fields required by the webhook server.
func (c *Config) ValidateServerMode() error {
	if c.GitHubWebhookSecret == "" {
		return errors.New("GITHUB_WEBHOOK_SECRET is required in server mode")
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
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Printf("warning: invalid value for %s, using default %d\n", key, fallback)
		return fallback
	}
	return n
}

func getEnvSlice(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}
