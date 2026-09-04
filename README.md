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


## Usage

```
spacefinder [path]
```
