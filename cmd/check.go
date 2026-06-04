package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/ui"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:     "check",
	Aliases: []string{"check-dependencies", "doctor"},
	Short:   "Verify the jira CLI is installed, authenticated, and reachable",
	RunE:    runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	plain := ui.IsPlain(out)
	if plain {
		fmt.Fprintln(out, "jirawk · environment check")
	} else {
		fmt.Fprintf(out, "%s %s\n", "🦅", ui.Title(plain, "jirawk · environment check"))
	}
	fmt.Fprintln(out)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	missing := 0
	jiraPath, lookErr := exec.LookPath(cfg.JiraBin)
	if lookErr == nil {
		printOK(out, plain, "jira", jiraPath)
	} else {
		printMissing(out, plain, "jira", "install jira-cli (https://github.com/ankitpokhrel/jira-cli)")
		missing++
	}

	if lookErr == nil {
		if err := jiraAuthStatus(cfg.JiraBin); err == nil {
			printOK(out, plain, "jira auth", "authenticated")
		} else {
			printMissing(out, plain, "jira auth", "run 'jira init' / check JIRA_API_TOKEN")
			missing++
		}
	}

	if cfg.Server != "" {
		printOK(out, plain, "server", cfg.Server)
	} else {
		printOptional(out, plain, "server", "no server detected; browse links will be bare keys")
	}

	if path := browserOpener(); path != "" {
		printOK(out, plain, "browser", path)
	} else {
		printOptional(out, plain, "browser", "no 'open'/'xdg-open'; issue links won't auto-open")
	}

	fmt.Fprintln(out)
	if missing > 0 {
		return fmt.Errorf("%d required dependency/dependencies missing", missing)
	}
	fmt.Fprintf(out, "%s all required dependencies present\n", ui.OK(plain))
	return nil
}

// jiraAuthStatus runs a cheap authenticated query to confirm credentials work.
func jiraAuthStatus(bin string) error {
	c := exec.Command(bin, "me")
	c.Stdout = nil
	c.Stderr = nil
	return c.Run()
}

func browserOpener() string {
	if runtime.GOOS == "darwin" {
		if p, err := exec.LookPath("open"); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("xdg-open"); err == nil {
		return p
	}
	if v := os.Getenv("BROWSER"); v != "" {
		return v
	}
	return ""
}

func printOK(out io.Writer, plain bool, name, detail string) {
	if plain {
		fmt.Fprintf(out, "  [OK]       %-12s %s\n", name, detail)
		return
	}
	fmt.Fprintf(out, "  %s  %-12s %s\n", ui.OK(false), name, ui.Dim(false, detail))
}
func printMissing(out io.Writer, plain bool, name, detail string) {
	if plain {
		fmt.Fprintf(out, "  [MISSING]  %-12s %s\n", name, detail)
		return
	}
	fmt.Fprintf(out, "  %s  %-12s %s\n", ui.Err(false), name, detail)
}
func printOptional(out io.Writer, plain bool, name, detail string) {
	if plain {
		fmt.Fprintf(out, "  [OPTIONAL] %-12s %s\n", name, detail)
		return
	}
	fmt.Fprintf(out, "  %s  %-12s %s\n", ui.Warn(false), name, ui.Dim(false, detail))
}
