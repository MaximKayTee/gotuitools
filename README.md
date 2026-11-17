# gotuitools

Shared packages for the Bubble Tea v2 programs in `~/Projects/Current`.

## Use

```
require github.com/MaximKayTee/gotuitools v0.0.0
replace github.com/MaximKayTee/gotuitools => ../gotuitools
```

`go doc github.com/MaximKayTee/gotuitools/<package>` for each API.

## Packages

| Package | What |
|---|---|
| `kitty` | Kitty graphics escapes: transmit, unicode placeholders, placements, cell pixel size |
| `termimg` | Encoder seam (`Kitty`, `Blank`), out-of-band `Queue`, decode, `Normalize`, resize and `Pack`, `Fit` to cells, `LRU`, `IDs`, `Circle`, `Cover` |
| `termout` | One serialised terminal writer for Bubble Tea and image queues; holds output until the alt screen |
| `mpv` | libmpv by dlopen, software-rendered frames |
| `player` | Video with sound in a cell rect: mpv frames as kitty images, controls, state; the frame `Sink` |
| `theme` | Helix-format themes, scopes with fallback; built in: charmtone, charmtone-light, terminal; `Register` a program's own |
| `text` | Widths, fit, truncate, wrap, rows, columns; bytes, durations, clocks, relative times, counts |
| `input` | One-line editor with word motions, paste, mask |
| `list` | Cursor list, variable-height scroller, scroll thumb |
| `fuzzy` | Ranked search: tiers by field, then subsequence |
| `command` | Command registry, key bindings by mode, chords, user rebinding, dispatcher |
| `palette` | Command palette and any chooser: a modal |
| `help` | Status-bar hints, keys screen, KEYS.md render and check |
| `modal` | Component and Modal contract, modal stack with message routing, `Frame`, overlays |
| `layout` | Rects, mouse zones, dividers, pointer shapes, "Window too small", resize settle |
| `settings` | One TOML file with every setting, declared as a tagged struct; typed values, bounds, enums, checks, atomic writes, reload; `[keys]` as one setting per command; layers (`Over`) |
| `settingsui` | Settings screen over a `settings.Source`: search, typed editors, saves each change |
| `sys` | Clipboard (OSC 52, wl-copy, xclip), paste, xdg-open, editor launch at a line, detach |

## Conventions

- Settings: `settings.Open(path, defaults)` at `<UserConfigDir>/<program>/config.toml`; then `Keys("keys", modes)` and `Fill()`, so the file holds every setting and lists the default keys. Edited in `settingsui`, never as text in the program. `e` in the settings screen opens the file in `$EDITOR`. A per-project file over it: `Over("System", system.Get)`.
- Commands: every action is a `command.Command`; keys, palette, help screen and hints read the registry. User keys in the settings file `[keys]` table: keys → command.
- Theme: a program registers its look as a theme named after it (and `<name>-light` when it has one) and takes `theme` from its settings. gotuitools components draw with the `ui.*` scopes listed in `go doc github.com/MaximKayTee/gotuitools/theme`.
- Keys: `ctrl+p` palette, `?` or `f1` keys screen, `ctrl+,` settings, unless a program already binds them.
- Modals: `modal.Stack` in the root model; `Update` it first; `View` over the screen last.
