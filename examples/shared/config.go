package shared

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// GetEnv returns an environment variable or a default value.
func GetEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// GetEnvInt returns an environment variable as int or a default value.
func GetEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

// GetEnvInt64 returns an environment variable as int64 or a default value.
func GetEnvInt64(key string, defaultValue int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

// GetEnvDuration returns an environment variable as duration or a default value.
func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultValue
}

// GetEnvBool returns an environment variable as bool or a default value.
func GetEnvBool(key string, defaultValue bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}

// BaseConfig contains common configuration options.
type BaseConfig struct {
	Port            string
	LogLevel        string
	DefaultMemoryMB uint64
	DefaultTimeout  time.Duration
	MaxConcurrent   int
}

// LoadBaseConfig loads base configuration from environment.
func LoadBaseConfig() BaseConfig {
	return BaseConfig{
		Port:            GetEnv("PORT", "8080"),
		LogLevel:        GetEnv("LOG_LEVEL", "info"),
		DefaultMemoryMB: uint64(GetEnvInt64("DEFAULT_MEMORY_MB", 256)),
		DefaultTimeout:  GetEnvDuration("DEFAULT_TIMEOUT", 30*time.Second),
		MaxConcurrent:   GetEnvInt("MAX_CONCURRENT", 10),
	}
}

// GetCacheDir returns a directory for caching filesystem snapshots.
// Uses CC_CACHE_DIR env var, or falls back to user cache directory.
func GetCacheDir() string {
	if dir := os.Getenv("CC_CACHE_DIR"); dir != "" {
		return dir
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(cacheDir, "cc", "snapshots")
}
