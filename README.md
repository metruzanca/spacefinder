# spacefinder

Visualize disk usage as an interactive **treemap** in your terminal — a
text-mode take on [spacesniffer](https://uderzo.it/main_products/space_sniffer/)
and [filelight](https://apps.kde.org/filelight/).

spacefinder scans a directory tree, sizes every folder recursively, and renders
the tree as nested squares spanning the whole screen. The biggest consumers
jump out instantly; drill in, spot the junk, and delete it — but only after you
type its name to confirm.

## Usage

```
spacefinder [path]
```

- With no argument, the scan starts at `$HOME` — or at `/` when run as root
  (`sudo spacefinder`).
- Pass a path to scan somewhere specific, e.g. `spacefinder /var/log`.

The full scan happens up front (the splash shows a spinner, the current path,
and entry counts), so drilling down is instant.

### Controls

| Key | Action |
| --- | --- |
| `↑ ↓ ← →` / `h j k l` | move the selection between squares |
| `Enter` | drill into the selected directory |
| `Esc` | go up to the parent |
| `Delete` / `Backspace` | open the delete prompt |
| `r` | re-scan the root |
| `q` / `Ctrl+C` | quit |

Deleting always asks you to **type the exact name** of the entry inside a modal
dialog — accidental deletions require real intent. The scan root itself can
never be deleted through the UI.

## Features

- **Spacesniffer-style rendering** via `bubbles`/`bubbletea` on the alternate
  screen; rectangles are laid out with a squarified treemap algorithm and
  colored deterministically.
- **Top-`N` + "other" bucketing.** Directories with hundreds of children show
  their largest ~128 entries as individual squares (the rest collapse into a
  single gray `other` tile) so the view stays fast and legible.
- **Accurate recursive sizes.** Every folder shows its true on-disk footprint,
  so the treemap reflects actual storage usage. Symlinks are counted but never
  followed, so scans can't loop.
- **Permission-tolerant.** Unreadable entries are skipped and counted; the
  scan keeps going and the count is shown on the splash.
- **Instant drill-down.** A single recursive scan builds the whole tree up
  front; navigation, breadcrumbs, and deletion re-lay out from memory.

## Development

- `go build ./...` — build
- `go vet ./...` — vet
- `go test ./...` — test (table-driven + `-update` golden files under
  `internal/{treemap,tui}/testdata/`)
- Debug logging is file-based and off by default (`GO_CLI_DEBUG=1`, path via
  `GO_CLI_LOG`) so it never corrupts the TUI output.

## How the pieces fit

- `internal/scan` — recursive directory tree with accumulated sizes and
  cancellable progress.
- `internal/treemap` — the squarified layout algorithm over a cell grid.
- `internal/tui` — the bubbletea app: splash, treemap rendering, navigation,
  and the type-to-confirm delete modal.
- `cmd/root.go` — Cobra entrypoint; picks the default root and starts the TUI.