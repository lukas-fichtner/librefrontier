package common

import (
	"os"
	"sync"
)

type Config struct {
	mu           sync.RWMutex
	apiBaseUrl   string
	dbConnString string
}

func (c *Config) GetApiBaseUrl() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiBaseUrl
}

// SetApiBaseUrl sets the base URL if not already configured (useful when LF_API_BASE_URL is empty).
func (c *Config) SetApiBaseUrl(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.apiBaseUrl == "" {
		c.apiBaseUrl = url
	}
}

func (c *Config) GetDbConnString() string {
	return c.dbConnString
}

func NewEnvConfig() *Config {
	c := Config{
		apiBaseUrl:   os.Getenv("LF_API_BASE_URL"),
		dbConnString: os.Getenv("LF_DB_CONN_STRING"),
	}

	return &c
}
