package app

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port           string
	WrapperAPIKey  string
	UpstreamBaseURL string
	UpstreamAPIKey  string
	RequestTimeout  time.Duration
	HTTPTimeout     time.Duration
	MaxImageBytes   int64
}

func LoadConfig() Config {
	return Config{
		Port:            env("PORT", "8080"),
		WrapperAPIKey:   strings.TrimSpace(os.Getenv("WRAPPER_API_KEY")),
		UpstreamBaseURL: strings.TrimRight(env("UPSTREAM_BASE_URL", "https://api.119337.xyz"), "/"),
		UpstreamAPIKey:  strings.TrimSpace(os.Getenv("UPSTREAM_API_KEY")),
		RequestTimeout:  secondsEnv("REQUEST_TIMEOUT_SECONDS", 300),
		HTTPTimeout:     secondsEnv("HTTP_TIMEOUT_SECONDS", 60),
		MaxImageBytes:   int64Env("MAX_IMAGE_BYTES", 30<<20),
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func secondsEnv(key string, fallback int) time.Duration {
	value, err := strconv.Atoi(env(key, ""))
	if err != nil || value <= 0 {
		value = fallback
	}
	return time.Duration(value) * time.Second
}

func int64Env(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(env(key, ""), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
