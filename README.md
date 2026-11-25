# gotuitools

Shared packages for the Bubble Tea v2 programs in `~/Projects/Current`.

## Use

```
go get github.com/MaximKayTee/gotuitools@main
```

`go doc github.com/MaximKayTee/gotuitools/<package>` for each API.

## Demo

```
go run github.com/MaximKayTee/gotuitools/cmd/gotuitools-demo@main
```

One tab per component: theme, text, input, list, fuzzy, modal, palette and files, layout, keys, settings, images. Not covered: `mpv`, `player`. Settings in `<UserConfigDir>/gotuitools-demo/config.toml`.

## Packages

| Package | What |
|---|---|
| `kitty` | Kitty graphics escapes: transmit, unicode placeholders, placements, cell pixel size |
| `termimg` | Encoder seam (`Kitty`, `Blocks` half blocks, `Blank`; `Choose` by mode), out-of-band `Queue`, decode, `Normalize`, resize and `Pack`, `Fit` to cells, `LRU`, `IDs`, `Circle`, `Cover` |
| `termout` | One serialised terminal writer for Bubble Tea and image queues; holds output until the alt screen |
| `mpv` | libmpv by dlopen; frames rendered on a GPU through a GL context with no window (libEGL by dlopen) and read back, by mpv's software renderer without one or with `Software`; decoding on the GPU; more properties followed as strings (`Observe`), more options (`Extra`); any mpv command (`Command`: a file queued after the one playing, no gap in the sound), any property set while it plays (`SetProperty`) |
| `player` | Video with sound in a cell rect: mpv frames as kitty images, through shared memory with `Shm`, controls, state; the frame `Sink` |
| `mpris` | A player on the session bus (MPRIS, godbus): media keys, headset buttons, playerctl; `Set` the state, read `Commands`. The name is held while a track is set |
| `awake` | Screensaver inhibit on the session bus (org.freedesktop.ScreenSaver, godbus) while video plays without a window; `Set(on)`, latest wins, never blocks |
| `pics` | Fetched pictures by key in boxes of cells: cut by kind (`Circle` icons, `Whole` covers), placed, freed when no longer drawn |
| `theme` | Helix-format themes, scopes with fallback; built in: charmtone, charmtone-light, terminal, catppuccin, catppuccin-light, gruvbox, gruvbox-light, tokyonight, nord, dracula; `Register` a program's own. A theme in `<UserConfigDir>/gotuitools/themes` is found by every program and can be a palette of eleven roles; a program lays its own parts under any theme with `Under` |
| `text` | Widths, fit, truncate, wrap, rows, columns; bytes, durations, clocks, relative times, counts |
| `input` | One-line editor with word motions, paste, mask |
| `list` | Cursor list, variable-height scroller, scroll thumb |
| `fuzzy` | Ranked search: tiers by field, then subsequence |
| `command` | Command registry, key bindings by mode, chords, user rebinding, dispatcher |
| `palette` | Command palette and any chooser: a modal; keys that act on the highlighted row |
| `form` | Modal of fields to fill in: text lines and choices, a check before submit |
| `files` | File or folder chooser on disk: a modal; path line, rows narrowed by typing, new names |
| `guide` | Quick start: a step to a page with text, keys and a picture or a live view of the program; ticked once its command has run, progress kept in a file; shown on the first start, then on F12 |
| `help` | Status-bar hints, keys screen, KEYS.md render and check |
| `modal` | Component and Modal contract, modal stack with message routing, `Frame`, overlays |
| `layout` | Rects, mouse zones, dividers, pointer shapes, "Window too small", resize settle |
| `settings` | One TOML file with every setting, declared as a tagged struct; typed values, bounds, enums, checks, atomic writes, reload; `[keys]` as one setting per command; layers (`Over`) |
| `settingsui` | Settings screen over a `settings.Source`: search, typed editors, saves each change |
| `sys` | Clipboard (OSC 52, wl-copy, xclip), paste, desktop notification (OSC 99), xdg-open, editor launch at a line, detach |

## Conventions

- Settings: `settings.Open(path, defaults)` at `<UserConfigDir>/<program>/config.toml`; then `Keys("keys", modes)` and `Fill()`, so the file holds every setting and lists the default keys. Edited in `settingsui`, never as text in the program. `e` in the settings screen opens the file in `$EDITOR`. A per-project file over it: `Over("System", system.Get)`.
- Commands: every action is a `command.Command`; keys, palette, help screen and hints read the registry. User keys in the settings file `[keys]` table: keys → command.
- Theme: a program registers its look as a theme named after it (and `<name>-light` when it has one) and takes `theme` from its settings. gotuitools components draw with the `ui.*` scopes listed in `go doc github.com/MaximKayTee/gotuitools/theme`.
- Keys: `ctrl+p` palette, `?` or `f1` keys screen (`help.Command`), `f12` guide (`guide.Command`), `ctrl+,` settings, unless a program already binds them. Each is a command's default key, changed in `[keys]`.
- Guide: steps start with `guide.Welcome`; `guide.Open` on `<UserConfigDir>/<program>/guide.toml`, `Registry.Ran` set to its `Ran`, opened when `First()`.
- Modals: `modal.Stack` in the root model; `Update` it first; `View` over the screen last.
