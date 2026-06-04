// Package config resolves jirawk's runtime settings: where the `jira` binary
// is, the Jira server base URL (for browse links), and UI tunables. Settings
// come from (highest priority first) env vars, an optional YAML config file,
// and autodetection from the jira-cli config.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	defaultRefreshInterval = 30 * time.Second
	defaultDoneWindow      = 30 * 24 * time.Hour
	defaultWeeks           = 8
)

// Config aggregates user-tunable settings.
type Config struct {
	JiraBin         string
	Server          string
	StatusClause    string
	RefreshInterval time.Duration
	DoneWindow      time.Duration
	Weeks           int
}

type fileConfig struct {
	JiraBin         string `koanf:"jira_bin"`
	Server          string `koanf:"server"`
	StatusClause    string `koanf:"in_progress_jql"`
	RefreshInterval string `koanf:"refresh_interval"`
	DoneWindow      string `koanf:"done_window"`
	Weeks           int    `koanf:"weeks"`
}

// Load builds a Config from defaults, the optional YAML file, env overrides,
// and jira-cli server autodetection.
func Load() (*Config, error) {
	cfg := &Config{
		JiraBin:         "jira",
		RefreshInterval: defaultRefreshInterval,
		DoneWindow:      defaultDoneWindow,
		Weeks:           defaultWeeks,
	}
	if err := mergeFileConfig(cfg, configFilePath()); err != nil {
		return nil, err
	}
	applyEnv(cfg)
	if cfg.Server == "" {
		cfg.Server = detectJiraServer()
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("JIRAWK_JIRA_BIN"); v != "" {
		cfg.JiraBin = v
	}
	if v := os.Getenv("JIRAWK_SERVER"); v != "" {
		cfg.Server = v
	}
	if v := os.Getenv("JIRA_SERVER"); v != "" && cfg.Server == "" {
		cfg.Server = v
	}
}

func configFilePath() string {
	if v := os.Getenv("JIRAWK_CONFIG"); v != "" {
		return v
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "jirawk", "config.yml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "jirawk", "config.yml")
}

func mergeFileConfig(cfg *Config, path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat config: %w", err)
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var fc fileConfig
	if err := k.Unmarshal("", &fc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if fc.JiraBin != "" {
		cfg.JiraBin = fc.JiraBin
	}
	if fc.Server != "" {
		cfg.Server = fc.Server
	}
	if fc.StatusClause != "" {
		cfg.StatusClause = fc.StatusClause
	}
	if fc.Weeks > 0 {
		cfg.Weeks = fc.Weeks
	}
	if fc.RefreshInterval != "" {
		d, err := time.ParseDuration(fc.RefreshInterval)
		if err != nil {
			return fmt.Errorf("config refresh_interval %q: %w", fc.RefreshInterval, err)
		}
		cfg.RefreshInterval = d
	}
	if fc.DoneWindow != "" {
		d, err := time.ParseDuration(fc.DoneWindow)
		if err != nil {
			return fmt.Errorf("config done_window %q: %w", fc.DoneWindow, err)
		}
		cfg.DoneWindow = d
	}
	return nil
}

// detectJiraServer reads the `server` field from the jira-cli config so browse
// links work without extra setup. Returns "" when it can't be found.
func detectJiraServer() string {
	path := os.Getenv("JIRA_CONFIG_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, ".config", ".jira", ".config.yml")
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return ""
	}
	return k.String("server")
}
