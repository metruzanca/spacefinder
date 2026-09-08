# Spacefinder Spec

## Overview

Spacefinder is a terminal TUI for finding and removing storage bottlenecks.
Inspired by SpaceSniffer (Windows) and Filelight (Linux), it renders a disk
or partition as a treemap of squares whose sizes reflect each folder's disk
usage. The application is built on [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea)
and runs on the alternate screen buffer.

The user navigates the treemap with arrow keys, Vim keys, or the mouse, drills
into a block with `Enter` (or a double-click), opens files in their OS default
application, and deletes unwanted entries. Breadcrumbs show the current path at
the top of the screen and keybind help is displayed at the bottom.

## Objective

Visually surface where the majority of storage is consumed so the user can
locate bottlenecks and, if desired, delete them.

## Root of the Scan

- Without a `path` argument, spacefinder opens a **picker** listing scan
  targets: the user's home directory (pre-selected), the filesystem root `/`,
  the directory spacefinder was launched from, and every other mounted
  filesystem worth scanning (physical partitions, secondary drives, removable
  media, and network mounts — pseudo/virtual filesystems like `proc`, `tmpfs`,
  or `overlay` are filtered out, and bind mounts of an already-listed device
  are collapsed into it). Drives show their free space; the current directory
  shows a rough top-level `du` estimate (run in the background so the picker
  never blocks on it).
- `enter` on the picker starts scanning the highlighted path; `q` or `Ctrl+C`
  quits (`esc` does nothing on this first screen, so it can never accidentally
  dismiss the picker).
- With a `path` argument, the scan starts there directly, exactly as before.
- Mount detection reads `/proc/mounts` on Linux, `getfsstat` on macOS, and the
  drive-letter bitmap on Windows.

## Navigation

- The treemap renders the full capacity of the selected path, so the largest
  blocks represent the heaviest consumers.
- Arrow keys or Vim keys (`h`/`j`/`k`/`l`) move the selection between blocks,
  snapping between block edges rather than by cell.
- `Enter` drills down into the selected directory, re-rendering the treemap
  for that path; it is a no-op on a file.
- `Esc` steps back up one level (and cancels a measurement still in flight).
- `r` rescans the root from scratch.
- `q` (or `Ctrl+C`) quits.
- `?` opens a modal with the full keybind reference and an explanation of what
  the mouse can do; `esc`, `enter`, or `?` closes it. The bottom bar shows the
  core shortcuts at a glance and note that the mouse is supported.
- Breadcrumbs at the top of the screen show the current location.

## Paging

A level with more children than can be shown at a legible minimum size is
split into pages, turned explicitly with `pgup` / `pgdn` or by activating a
full-height arrow gutter on the edge of the map, so no directory entry is ever
hidden away:

- All pages share one bytes→cells scale (the whole level's), so items keep
  their true relative size across pages: page 1 holds the biggest entries, the
  last page the smallest, each drawn at that shared scale down to the minimum
  tile size.
- Every rendered tile keeps at least a 3×3 footprint (a name label needs ≥3
  rows and ≥3 columns); items are clamped up to that floor, so a tiny entry
  still renders as a readable block rather than a hairline. A page that would
  end in a too-thin partial row is split so its items move to the next page.
- Because later pages hold genuinely small items, they are usually sparse: the
  unused area renders as plain terminal background (the empty tail of the map).
- Each paged level gets a 3-column gutter on the right edge of the map (next
  page) and, on all but the first page, one on the left edge (previous page),
  drawn as a full-height strip with a centred arrow glyph. The gutters are
  ordinary selectable cells: arrow/Vim keys and the mouse wheel reach them, and
  the layout is chunked at the surviving width so every page stays legible
  inside its gutters. `Enter` (or a double-click) on a gutter flips the page;
  a single click only selects it. Selection never wraps across page edges — the
  gutters are the explicit boundary.
- The status line shows `page X/Y` when a level spans more than one page.
- Only zero-size entries are dropped entirely (surfaced as a hidden count);
  everything else is reachable somewhere.

## Mouse

Mouse cell motion is enabled on the alternate screen.

- Left-click selects the block or page-arrow gutter under the cursor.
- Double-click within the debounce window (~350 ms) on the same block opens
  the entry: directories drill in, files are launched in the OS default
  application via `open` (macOS), `xdg-open` (Linux), or
  `rundll32 url.dll,FileProtocolHandler` (Windows), and a page arrow flips the
  page. The opener runs detached, so a long-running app does not tie up the
  TUI; only launch failures surface as errors.
- Right-click goes up a level.
- The mouse wheel moves the selection up and down.

## Deletion

Pressing `Delete` (or `Backspace`) deletes the selected file or folder.
Deletion **always** requires confirmation: the user must type the exact name
of the entry they are deleting in a modal prompt before the action proceeds.
A mismatch shows an error and nothing is deleted. The scan root itself can
never be deleted through the UI.

## Scanning Strategy

The scan behaves like `du -x -B1 --max-depth=1` but is lazy:

- **Allocated blocks**: sizes use allocated blocks, not apparent size,
  matching `du`.
- **Single filesystem**: mount points are treated as leaves.
- **Hardlinks counted once**.
- **Lazy expansion**: the treemap materializes one level at a time; drilling
  into a folder is a `readdir` plus a look-up in the recorded totals
  (measure-then-open). No "unmeasured tiles" are rendered because every shown
  level carries real totals.
- **One fast measure pass**: records the total size of every directory without
  building a deep tree.
- **Throttled progress**: progress updates are throttled to ~20 events/s rather
  than one per entry, which previously deadlocked the scanner's 128-slot
  channel and made full home-directory scans appear to hang.
- **Re-measure on change**: folders changed since the measure pass are
  re-measured on the spot.
- **Deletion bookkeeping**: deleting an entry subtracts its real size up the
  visible ancestor chain and invalidates its recorded total, so a repeat of
  that folder is re-measured correctly.
- **UI feedback**: the scan shows a splash screen (spinner, current path, entry
  counts); drilling shows a brief measuring view; failures land on an error
  screen. `Esc` cancels a measurement, `q` quits from any state.

## Rendering

- **Squarified treemap**: the aspect heuristic is measured against the longest
  side of the free box, with strips laid along the correct axis; a prior
  inverted/second-axis bug degraded every level into a full-height bar chart.
- **Sqrt-scaled areas**: tile areas are sqrt-scaled so ranking and order stay
  truthful while a dominant folder no longer reduces its neighbours to
  hairlines; labels and percentages always show real block counts.
- **Edge-to-edge blocks**: blocks fill the whole area without borders; the
  selection is the only outlined block (accent box).
- **Adjacency-distinct colours**: tiles are coloured from a fixed palette of
  hues spread evenly around the colour wheel (no two palette colours are ever
  similar), and each tile is assigned the colour used by the fewest of its
  edge-adjacent neighbours — so two blocks sharing a border never render the
  same or a near-identical colour, keeping neighbouring tiles distinguishable.
- **Navigation by block edges**: arrow and wheel navigation moves by block
  edges — the chosen neighbour is the nearest block strictly beyond the
  current one in the pressed direction, best aligned across the other axis.
- **Free-space gutter**: a gutter row (statfs free bytes on the partition)
  shows at the scan root only, capped to a few rows so the content map keeps
  the screen.
- **Minimum tile size + pages**: no entry is capped away; instead the level is
  paginated at one shared scale so every rendered tile keeps a legible 3×3
  footprint and anything that would not fit legibly on the current page moves
  to the next one, with the small tail drawn as smaller (sparser) pages. See
  "Paging" above.
- **Info row**: a single row below the breadcrumbs describes the current
  selection (name, size, share) and, pinned to the right, the level's children
  count, hidden (zero-size) count, and page number (`page X/Y`) when paged;
  the bottom line is reserved for keybinds, rendered on a black bar with white
  keys and neutral gray descriptions.
