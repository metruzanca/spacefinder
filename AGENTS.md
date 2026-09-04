## Project

Go CLI application built with [Cobra](https://github.com/spf13/cobra). Used as a starting template for new CLI projects.

For detailed context on what this application does, see [docs/spec.md](docs/spec.md).

## Structure

- `main.go` — thin entrypoint; loads godotenv, inits debug logging, calls `cmd.Execute()`
- `cmd/` — Cobra command definitions; business logic lives in `internal/`, not here
- `internal/logging/` — env-gated debug logging to a file (off by default, `GO_CLI_DEBUG`)
- `internal/config/` — optional TOML config, created on first use (`GO_CLI_CONFIG_PATH` override); wired in `main.go` as commented-out code
- `.goreleaser.yaml` — release config (linux/darwin/windows, amd64/arm64) that injects version via ldflags
- `.github/workflows/release.yml` — tags a `v*` run GoReleaser and publish GitHub Releases

## Commands

- `go build ./...` — build
- `go vet ./...` — vet
- `go test ./...` — test
- `goreleaser check` — validate release config
- `goreleaser release --snapshot --clean` — local test release

## Recommended packages

- **Configuration** — [BurntSushi/toml](https://github.com/BurntSushi/toml). Config file at `os.UserConfigDir()/spacefinder/config.toml` (e.g. `~/.config/spacefinder/config.toml`), generated on first use if it doesn't exist. Do NOT use the "warning: missing config, run init first" pattern. If an init command needs user input, still auto-run it on first use (detect missing config), in addition to exposing it as a command. Support an env var to override the config location (e.g. `GO_CLI_CONFIG_PATH`) so dev can point it at a repo-local file. Optional pattern: find-up (walk parent dirs) for project-local config, failing open to defaults — never warn about a missing config.
- **Environment overrides** — [joho/godotenv](https://github.com/joho/godotenv) to load env vars at dev time that override config options or config location. Optionally, load a local config file instead.
- **UI/pretty output** — [charmbracelet](https://github.com/charmbracelet) packages: `lipgloss`, `bubbletea`, `glow`, `bubbles` (common components), etc. `charm.land/lipgloss/v2` (with its `table` subpackage) is a valid alternative. Only style output when stdout is a terminal.
- **Prompts/forms** — [charmbracelet/huh](https://github.com/charmbracelet/huh). Treat `huh.ErrUserAborted` as a silent cancel — never an error exit.
- **README demos** — [charmbracelet/vhs](https://github.com/charmbracelet/vhs) to record terminal demos (.tape scripts → GIF/ASCIICast) for a pretty README, and [charmbracelet/vhs-action](https://github.com/charmbracelet/vhs-action) to render them in CI. Commit the generated GIFs so the README renders without tooling; deterministic via `Set FontSize`, fixed dimensions, and `Type @0ms`/`Sleep` for predictable pacing.

## Output & errors

- stdout is the pipe target — keep it clean. Errors go to stderr, never stdout.
- Only emit ANSI styling when stdout is a terminal (`term.IsTerminal` or charmbracelet's style detection); piped output stays plain and deterministic.
- Exit codes: `0` ok, `1` runtime error, `2` usage error.
- Cobra: use `RunE` and return errors; set `SilenceUsage: true` so runtime errors don't dump help text.

## Logging

- Write logs to a file, never stdout — logs must never disrupt the CLI/TUI output.
- Gated by an env var and off by default; the same var can override the log path. Announce the path on stderr only when logging initializes.
- Location: in the repo during dev, and `os.UserConfigDir()/spacefinder/` (e.g. `~/.config/spacefinder/`) in production.

## Testing

- Table-driven tests next to source, in the same package.
- Golden-file helper (`golden(t, name, got)`) with a `-update` flag for byte-exact render output; fixture files for parser tests.
- Hermetic: `t.Setenv` for env-dependent code, `t.TempDir` for temp paths.

## Conventions

- Release versions are tagged `v*` (e.g. `v0.1.0`)
- Version is baked in at build time via ldflags (`cmd.Version`) and surfaced through Cobra's built-in `--version`, not hardcoded
- Commit messages use conventional prefixes (`feat:`, `fix:`, `chore:`, `docs:`, `test:`, `ci:`) so the GoReleaser changelog groups cleanly
