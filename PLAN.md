Inspired by spacesniffer on windows and filelight on linux. Spacefinder is a TUI based application using charmbracelet's bubbletea. It uses alt-screen and will display UI similar to spacesniffer which renders the drive as squares of varrying size, using the whole screen to represent the full disk (or partition) capacity. Then using arrow keys or vim keys the user can select a square and press enter to drill down. When drilling down we do the same thing but at that new path. We should have breadcrumbs at the top of the screen and at the bottom user input keybinds. A user can also press del on their keybord (or backspace) to delete a given folder/file. This should ALWAYS prompt the user with a modal asking them to type the name of the file/folder they want to delete.

If the program is run as a user, it should by default use $HOME as the root of the scan. If they use `sudo spacefinder` it should use /.

The goal of spacefinder is to visually help the user find where their main storage bottlenecks are and if they want, delete them.

## Scanning strategy (implemented)

The scan behaves like `du -x -B1 --max-depth=1`, but lazily:

- Sizes are **allocated blocks** (not apparent size), matching `du`, across a
  **single filesystem** (mount points are treated as leaves), with **hardlinks
  counted once**.
- One fast measure pass records the total size of every directory; it does not
  build a deep tree. Progress is **throttled** (~20 events/s), never one vsync
  per entry — that per-entry progress was the reason a full home-dir scan
  appeared to hang (a full 128-slot channel deadlocked the scanner).
- **Lazy expansion**: the treemap materializes one level at a time. Drilling
  into a folder is a readdir plus a look-up in the recorded totals
  (measure-then-open); folders changed since the pass are re-measured on the
  spot. No "unmeasured tiles" are needed because every displayed level carries
  real totals.
- Deletion subtracts the removed entry's real size up the visible ancestor
  chain and invalidates its recorded total so a repeat of that folder is
  re-measured correctly.
