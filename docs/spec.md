# Spacefinder Spec

## Overview

Spacefinder is a terminal TUI for finding and removing storage bottlenecks.
Inspired by SpaceSniffer (Windows) and Filelight (Linux), it renders a disk
or partition as a treemap of squares whose sizes reflect each folder's disk
usage. The application is built on [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea)
and runs on the alternate screen buffer.

The user navigates the treemap with arrow keys or Vim keys, drills into a
block with `Enter`, and deletes unwanted entries. Breadcrumbs show the current
path at the top of the screen and keybind help is displayed at the bottom.

## Objective

Visually surface where the majority of storage is consumed so the user can
locate bottlenecks and, if desired, delete them.

## Root of the Scan

- When run as a normal user, the scan root defaults to `$HOME`.
- When run with `sudo spacefinder`, the scan root defaults to `/`.

## Navigation

- The treemap renders the full capacity of the selected path, so the largest
  blocks represent the heaviest consumers.
- Arrow keys or Vim keys (`h`/`j`/`k`/`l`) move the selection between blocks.
- `Enter` drills down into the selected block, re-rendering the treemap for
  that path.
- Breadcrumbs at the top of the screen show the current location.

## Deletion

Pressing `Delete` (or `Backspace`) deletes the selected file or folder.
Deletion **always** requires confirmation: the user must type the exact name
of the entry they are deleting in a modal prompt before the action proceeds.

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

## Rendering

- **Squarified treemap**: the aspect heuristic is measured against the longest
  side of the free box, with strips laid along the correct axis; a prior
  inverted/second-axis bug degraded every level into a full-height bar chart.
- **Sqrt-scaled areas**: tile areas are sqrt-scaled so ranking and order stay
  truthful while a dominant folder no longer reduces its neighbours to
  hairlines; labels and percentages always show real block counts.
- **Edge-to-edge blocks**: blocks fill the whole area without borders; the
  selection is the only outlined block (accent box).
- **Navigation by block edges**: arrow and wheel navigation moves by block
  edges — the chosen neighbour is the nearest block strictly beyond the
  current one in the pressed direction, best aligned across the other axis.
- **Free-space gutter**: a gutter row (statfs free bytes on the partition)
  shows at the scan root only, capped to a few rows so the content map keeps
  the screen.
