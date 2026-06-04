package cmd

import (
	"time"

	"github.com/figarocorso/jirawk/internal/tui"
)

func runTUI() error {
	return runTUIWatch(0)
}

func runTUIWatch(interval time.Duration) error {
	cfg, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	return tui.Run(cfg, client, interval)
}
