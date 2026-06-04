# 🦅 jirawk

> **Keep watch over your Jira issues.**

`jirawk` (jira-hawk) is a single-binary CLI + TUI for keeping tabs on the Jira
issues assigned to you. It needs no tracked list — everything is derived live
from JQL through [`jira-cli`][jira-cli]. Two things at a glance:

- the issues you have **in progress** (across every project), and
- the issues you **closed in the last 24 hours**.

Run `jirawk stats` for an in-progress count plus an 8-week bar chart of how many
issues you've resolved per week.

## Features

- Interactive Bubble Tea TUI: two stacked tables (in progress / recently
  closed), `tab` to switch focus, `enter` to open an issue in the browser.
- One-shot non-interactive `jirawk list` (use `--json` for agents).
- `jirawk stats` — in-progress count + weekly resolved bar chart.
- `jirawk watch` — TUI with periodic auto-refresh.
- Queries span **all projects** (not just your jira-cli default project).
- Plain/ASCII output for pipes, agents, and CI (`--plain` / `NO_COLOR`).
- Optional MCP server (`jirawk mcp`) so AI agents can query your issues.

## Requirements

- [`jira-cli`][jira-cli] (`jira`), authenticated (`jira init` once). jirawk
  shells out to it for every query and inherits its auth and server config.

Optional:

- `open` (macOS) / `xdg-open` (Linux) — open issues in the browser on `Enter`.
- `pbcopy` / `wl-copy` / `xclip` / `xsel` — copy the issue URL to the clipboard.

Verify your setup:

```sh
jirawk check
```

## Install

### Homebrew (macOS / Linux)

```sh
brew install figarocorso/tap/jirawk
jira init          # configure jira-cli if you haven't already
jirawk check
```

### `go install`

```sh
go install github.com/figarocorso/jirawk@latest
jirawk check
```

### From source

```sh
git clone https://github.com/figarocorso/jirawk.git
cd jirawk
go build -o jirawk .
sudo mv jirawk /usr/local/bin/jirawk
```

## Usage

```sh
jirawk                              # interactive TUI (in progress + closed 24h)
jirawk watch [--interval 30s]       # TUI + periodic auto-refresh
jirawk list                         # both sections as a plain table
jirawk list --section in-progress   # only in-progress issues
jirawk list --section done          # only recently-closed issues
jirawk list --json                  # JSON object (agent-friendly)
jirawk stats                        # in-progress count + weekly closed chart
jirawk stats --weeks 12             # change the look-back window
jirawk get OP-649                   # single-issue detail
jirawk check                        # environment / auth check
jirawk version
```

### Example

```text
$ jirawk stats
jirawk · usage

  in progress : 23

  closed — last 8 weeks (18 total)
    Apr 09–Apr 16   ███████████████ 2
    Apr 23–Apr 30   ██████████████████████ 3
    May 07–May 14   ██████████████████████████████ 4
    ...
  ▸ May 28–Jun 04   ███████ 1
```

### Output styling

Human terminals get colored, emoji-prefixed output. Pipes, `NO_COLOR=1`, and
`--plain` (alias `--no-color`) force ASCII-only output that is safe for AI
agents, scripts, and CI logs. `--json` is unaffected.

## How it works

jirawk builds JQL and runs it through `jira issue list --raw`, then parses the
JSON. Every query is scoped with `project IS NOT EMPTY AND assignee =
currentUser()` so it spans **all** projects you touch (jira-cli otherwise
restricts a raw `-q` to its default project). The sections use:

- **in progress** — `statusCategory = "In Progress"`
- **closed (24h)** — `statusCategory = Done AND resolved >= -24h`
- **weekly stats** — one count query per week over the look-back window

jira-cli's `--raw` output omits the real `statusCategory`, so jirawk derives a
coarse category (in progress / in review / done / to do / blocked) from the
status name for coloring.

## Configuration

jirawk works with zero config. An optional YAML file at
`${XDG_CONFIG_HOME:-~/.config}/jirawk/config.yml` overrides defaults:

```yaml
jira_bin: jira                          # path to the jira binary
server: https://acme.atlassian.net      # browse-link base (autodetected from jira-cli)
in_progress_jql: statusCategory = "In Progress"   # override the in-progress predicate
refresh_interval: 30s
done_window: 24h
weeks: 8
```

Env overrides: `JIRAWK_JIRA_BIN`, `JIRAWK_SERVER`, `JIRAWK_CONFIG`. The Jira
server base for browse links is autodetected from the jira-cli config
(`~/.config/.jira/.config.yml`) when not set.

## AI / agent integration (MCP server)

jirawk ships a stdio [Model Context Protocol][mcp] server:

```sh
jirawk mcp
```

| Tool          | Purpose                                                        |
| ------------- | -------------------------------------------------------------- |
| `list_issues` | List your issues. `section` = in-progress / done / all.        |
| `get_issue`   | Full detail for a single issue by key.                         |
| `stats`       | In-progress count + weekly resolved breakdown.                 |

### Claude Code / Claude Desktop config

```json
{
  "mcpServers": {
    "jirawk": {
      "command": "jirawk",
      "args": ["mcp"]
    }
  }
}
```

[jira-cli]: https://github.com/ankitpokhrel/jira-cli
[mcp]: https://modelcontextprotocol.io

## License

Apache License 2.0 — see [`LICENSE`](./LICENSE).
