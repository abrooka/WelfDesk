package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const Version = "0.1.0"

type Provider struct {
	Kind      string `json:"kind"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey,omitempty"`
	Model     string `json:"model"`
	MaxTokens int    `json:"maxTokens"`
}

type Config struct {
	DataDir        string   `json:"dataDir"`
	Listen         string   `json:"listen"`
	PermissionMode string   `json:"permissionMode"`
	MaxTurns       int      `json:"maxTurns"`
	Provider       Provider `json:"provider"`
}

func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".weft"
	}
	return filepath.Join(home, ".weft")
}

func Defaults(dataDir string) Config {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	return Config{
		DataDir:        dataDir,
		Listen:         "127.0.0.1:47123",
		PermissionMode: "ask",
		MaxTurns:       24,
		Provider: Provider{
			Kind:      "mock",
			BaseURL:   "",
			Model:     "mock",
			MaxTokens: 8192,
		},
	}
}

func Load(dataDir string) (Config, error) {
	cfg := Defaults(dataDir)
	path := filepath.Join(cfg.DataDir, "config.json")
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, err
		}
	}
	applyEnv(&cfg)
	Normalize(&cfg)
	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(c.DataDir, "config.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(c.DataDir, "config.json"))
}

func (c Config) Masked() map[string]any {
	return map[string]any{
		"dataDir":        c.DataDir,
		"listen":         c.Listen,
		"permissionMode": c.PermissionMode,
		"maxTurns":       c.MaxTurns,
		"provider": map[string]any{
			"kind":      c.Provider.Kind,
			"baseUrl":   c.Provider.BaseURL,
			"model":     c.Provider.Model,
			"maxTokens": c.Provider.MaxTokens,
			"keySet":    c.Provider.APIKey != "",
		},
	}
}

func applyEnv(c *Config) {
	if v := os.Getenv("WEFT_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("WEFT_PERMISSION_MODE"); v != "" {
		c.PermissionMode = v
	}
	if v := os.Getenv("WEFT_PROVIDER"); v != "" {
		c.Provider.Kind = v
	}
	if v := os.Getenv("WEFT_BASE_URL"); v != "" {
		c.Provider.BaseURL = v
	}
	if v := os.Getenv("WEFT_API_KEY"); v != "" {
		c.Provider.APIKey = v
	}
	if v := os.Getenv("WEFT_MODEL"); v != "" {
		c.Provider.Model = v
	}
}

func Normalize(c *Config) {
	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = "127.0.0.1:47123"
	}
	switch c.PermissionMode {
	case "ask", "acceptEdits", "yolo":
	default:
		c.PermissionMode = "ask"
	}
	if c.MaxTurns <= 0 {
		c.MaxTurns = 24
	}
	c.Provider.Kind = strings.ToLower(strings.TrimSpace(c.Provider.Kind))
	if c.Provider.Kind == "" {
		c.Provider.Kind = "mock"
	}
	if c.Provider.MaxTokens <= 0 {
		c.Provider.MaxTokens = 8192
	}
	if c.Provider.Model == "" {
		c.Provider.Model = "mock"
	}
	switch c.Provider.Kind {
	case "openai":
		if c.Provider.BaseURL == "" {
			c.Provider.BaseURL = "https://api.openai.com/v1"
		}
	case "anthropic":
		if c.Provider.BaseURL == "" {
			c.Provider.BaseURL = "https://api.anthropic.com"
		}
	}
	c.Provider.BaseURL = strings.TrimRight(c.Provider.BaseURL, "/")
}
