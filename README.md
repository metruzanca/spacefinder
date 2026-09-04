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

| Input | Action |
| --- | --- |
| `↑ ↓ ← →` / `h j k l` | move the selection between squares |
| mouse click | select the square under the cursor |
| mouse double-click / `Enter` | drill into the selected directory |
| mouse wheel | move the selection |
| mouse right-click / `Esc` | go up to the parent |
| `Delete` / `Backspace` | open the delete prompt |
| `r` | re-scan the root |
| `q` / `Ctrl+C` | quit |

Deleting always asks you to **type the exact name** of the entry inside a modal
dialog — accidental deletions require real intent. The scan root itself can
never be deleted through the UI.

## Features

- **Spacesniffer-style rendering** via `bubbles`/`bubbletea` on the alternate
  screen; rectangles are laid out with a squarified treemap algorithm and
  colored deterministically. Blocks fill the whole area edge-to-edge with no
  borders; the selection is marked by brightening the block and a `▸` on its
  label, so the treemap stays fully visible even when a terminal disables
  colors (`NO_COLOR`).
- **Only significant blocks show.** Directory entries too small to earn a
  tile (a couple of cells) are folded into the gray, non-selectable `other`
  bucket and counted as `hidden` in the status line — they are neither drawn
  nor reachable with the arrow keys or mouse. spacefinder is for spotting the
  big consumers, not the tail.
- **Square-tile proportions.** Tile areas are square-root scaled from byte
  counts: ordering and ranking stay true, but a folder that dominates the disk
  no longer squashes every neighbour into hairline columns — the treemap stays
  legible as squares. Labels and percentages always show the true sizes.
- **Free-space gutter.** At the scan root, a muted band shows how much of the
  partition is still free (from `statfs`), e.g. `free · 1.2 TB`; drilling in
  hides it since free space belongs to the partition, not a folder.
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
  filesystem, hardlink dedup) that records every directory's total, per-level
  lazy `Expand` for the tree, and statfs free-space reporting.
- `internal/treemap` — the squarified layout algorithm over a cell grid.
- `internal/tui` — the bubbletea app: splash, treemap rendering, lazy
  navigation, the free-space gutter, and the type-to-confirm delete modal.
- `cmd/root.go` — Cobra entrypoint; picks the default root and starts the TUI.