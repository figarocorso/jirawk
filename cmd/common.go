package cmd

import (
	"fmt"
	"os/exec"

	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
)

// clientFactory builds a jira.Client from config. Overridden in tests.
var clientFactory = func(cfg *config.Config) (jira.Client, error) {
	// Cheap, no-network preflight: fail early with a pointer to `jirawk check`
	// when the jira CLI isn't on PATH, instead of surfacing an opaque exec
	// error on the first query.
	if _, err := exec.LookPath(cfg.JiraBin); err != nil {
		return nil, fmt.Errorf("jira CLI %q not found in PATH — run 'jirawk check' for diagnostics", cfg.JiraBin)
	}
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
