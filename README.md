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

The full home dir scan runs like `du`: one fast pass up front (the splash
shows a spinner, the current path, and entry counts), then drilling is
effectively instant.

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
- **`du`-style, lazy sizing.** The initial pass measures the whole tree like
  `du -x -B1`: sizes are allocated blocks, matching what `du` reports, one
  filesystem (mount points are treated as leaves), and each hardlink is
  counted once. Progress is throttled instead of repainting per file, so even
  a multi-million-entry home directory scans in a few seconds.
- **Instant drill-down.** One measure pass records the total of every
  directory; expanding a folder (readdir + lookup) is near-instant, so
  navigating in is never a rescan. Folders that changed after the pass are
  re-measured on the spot the moment you open them.
- **Permission-tolerant.** Unreadable entries are skipped and counted; the
  scan keeps going and the count is shown on the splash.

## Development

- `go build ./...` — build
- `go vet ./...` — vet
- `go test ./...` — test (table-driven + `-update` golden files under
  `internal/{treemap,tui}/testdata/`)
- Debug logging is file-based and off by default (`GO_CLI_DEBUG=1`, path via
  `GO_CLI_LOG`) so it never corrupts the TUI output.

## How the pieces fit

- `internal/scan` — du-style measure pass (allocated blocks, single
  filesystem, hardlink dedup) that records every directory's total, plus lazy
  per-level `Expand` for the tree.
- `internal/treemap` — the squarified layout algorithm over a cell grid.
- `internal/tui` — the bubbletea app: splash, treemap rendering, lazy
  navigation, and the type-to-confirm delete modal.
- `cmd/root.go` — Cobra entrypoint; picks the default root and starts the TUI.