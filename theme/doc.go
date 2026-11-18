// Package theme maps scopes, the names of parts of the screen, to styles.
//
// A scope is a dotted name such as "ui.selection" or "markup.heading.1".
// A scope with no style of its own falls back to the scope above it, made
// by trimming the last dotted part, until one has a style or none is left.
//
// Themes are TOML files in Helix's format: each key is a scope, each value
// a colour or a table of fg, bg and modifiers, with a [palette] of named
// colours and inherits naming a parent theme in the same directory or a
// registered theme.
//
// Built in: charmtone, charmtone-light, terminal, and as palettes of the
// roles catppuccin, catppuccin-light, gruvbox, gruvbox-light, tokyonight,
// nord, dracula. A program registers its own with Register; "<name>-light" is the light variant FindFor picks on
// a light terminal (tea.RequestBackgroundColor, BackgroundColorMsg.IsDark).
//
// # One theme for every program
//
// A theme file in <config>/gotuitools/themes is found by every program
// (Dirs). It names no program. It can be a palette of the roles and
// nothing more:
//
//	[palette]
//	text = "#ecebf0"        # plain text
//	background = "#201f26"  # the screen
//	surface = "#2d2c36"     # raised: bars, menus, blocks
//	border = "#3a3943"      # lines between things
//	muted = "#858392"       # text that matters less
//	accent = "#6b50ff"      # the theme's colour: selection, focus
//	accent2 = "#ff60ff"     # its second colour: titles, marks
//	good = "#00ffb2"
//	warning = "#f5ef34"
//	bad = "#ff577d"
//	info = "#00a4ff"
//
// The scopes below and markup.* are drawn from the roles (base.toml)
// unless the theme styles them itself. A program writes its own parts
// once, with the roles for colours, and lays them under the theme chosen:
//
//	th, _ = th.Under([]byte(`"gotube.mark" = { fg = "accent2", modifiers = ["bold"] }`))
//
// "onaccent" is one more name: text or background, whichever reads better
// on accent. A theme without a role in its palette has it from the scope
// drawn with it: accent from ui.window.focus, muted from ui.text.inactive.
//
// # Scopes used by gotuitools components
//
//	ui.background        screen bg
//	ui.text              plain text
//	ui.text.focus        focused or matched text, titles
//	ui.text.inactive     muted text, defaults, notes
//	ui.selection         selected row
//	ui.cursor            text cursor
//	ui.statusline        status bar
//	ui.statusline.key    key in a hint
//	ui.window            borders, rules
//	ui.window.focus      focused border
//	ui.popup             modal title
//	ui.menu              palette and list body
//	ui.menu.selected     highlighted palette row
//	error, warning, info, hint
//
// A program's own scopes are prefixed with its name: "gotube.mark".
package theme
