# spacefinder

![](.github/demo.png)

Visualize disk usage as an interactive **treemap** in your terminal — a
text-mode take on [spacesniffer](https://uderzo.it/main_products/space_sniffer/)
and [filelight](https://apps.kde.org/filelight/).

The full home dir scan runs like `du`: one fast pass up front (the splash
shows a spinner, the current path, and entry counts), then drilling is
effectively instant.

Deleting always asks you to **type the exact name** of the entry inside a modal
dialog — accidental deletions require real intent. The scan root itself can
never be deleted through the UI.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/metruzanca/spacefinder/main/install.sh | sh
```

Downloads the latest release binary for your OS/arch, verifies its checksum, and
installs it to `~/.local/bin` (override with `PREFIX` or `BINDIR`). Or build from
source:

```bash
go install github.com/metruzanca/spacefinder@latest
```

## Usage

```
spacefinder [path]
```

`path` defaults to user's home directory or root if root.
