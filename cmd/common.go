package cmd

import (
	"fmt"

	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
)

// clientFactory builds a jira.Client from config. Overridden in tests.
var clientFactory = func(cfg *config.Config) (jira.Client, error) {
	return jira.NewCLIClient(
		jira.WithBinary(cfg.JiraBin),
		jira.WithServer(cfg.Server),
		jira.WithStatusClause(cfg.StatusClause),
	), nil
}

func loadConfigAndClient() (*config.Config, jira.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	client, err := clientFactory(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("init client: %w", err)
	}
	return cfg, client, nil
}
