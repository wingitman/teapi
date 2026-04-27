# teapi (tee-pee) *(wip)*

A terminal UI (TUI) HTTP client.

---

## Features

- **Collections** — organise requests into named groups with an optional per-group base URL
- **Request builder** — supports GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS with URL, headers, body, and variable interpolation
- **Variables** — local (`{var}`), global (`{{var}}`), and built-in fakers (`{{$uuid}}`, `{{$timestamp}}`, `{{$name}}`, `{{$email}}`, and more)
- **Tests** — write assertions against responses (status code, body contains/equals, header equals, JSONPath equals)
- **Workflows** — chain sequential or parallel multi-step request pipelines with JSONPath variable extraction between steps
- **Batch runs** — run a request against every row of a CSV or TXT file
- **History** — scrollable log of recent requests
- **Editor integration** — open request/response bodies in `$EDITOR`
- **Clipboard** — copy responses, URLs, and headers with a single key
- **Fully remappable keybindings** via a TOML config file

---

## Requirements

- Go 1.21+
- A terminal with colour support

---

## Installation

### Linux / macOS

```sh
git clone https://github.com/wingitman/teapi
cd teapi
make install   # builds and copies binary to ~/.local/bin/teapi
```

### Windows

```powershell
.\install.ps1
```

Builds the binary and installs it to `%LOCALAPPDATA%\Programs\teapi`, updating your user `PATH` automatically (no admin required).

---

## Usage

```sh
teapi
```

On first launch, config and data files are created automatically:

| Platform | Config | Data |
|----------|--------|------|
| Linux | `~/.config/delbysoft/teapi.toml` | `~/.config/delbysoft/teapi.json` |
| macOS | `~/Library/Application Support/delbysoft/teapi.toml` | `~/Library/Application Support/delbysoft/teapi.json` |
| Windows | `%AppData%\Roaming\delbysoft\teapi.toml` | `%AppData%\Roaming\delbysoft\teapi.json` |

---

## Default keybindings

| Key | Action |
|-----|--------|
| `tab` / `shift+tab` | Cycle panels / sub-tabs |
| `s` | Send request / run workflow / run batch |
| `n` | New item |
| `d` | Delete item |
| `r` | Rename (sidebar) |
| `e` | Edit collection / load request |
| `y` | Copy focused content to clipboard |
| `E` | Open request body in `$EDITOR` |
| `R` | Open response body in `$EDITOR` |
| `o` | Open config file in `$EDITOR` |
| `N` | Add global variable |
| `up` / `down` | Navigate lists |
| `enter` | Confirm / enter edit mode |
| `esc` | Cancel / exit edit mode |
| `q` / `ctrl+c` | Quit |

All keybindings can be remapped in `teapi.toml`.

---

## Support

<a href='https://ko-fi.com/W7W21WP5L7' target='_blank'><img height='36' style='border:0px;height:36px;' src='https://storage.ko-fi.com/cdn/kofi4.png?v=6' border='0' alt='Buy Me a Coffee at ko-fi.com' /></a>

---

## License

MIT — see [LICENSE](LICENSE).

Copyright (c) 2026 [delbysoft](https://github.com/wingitman)
