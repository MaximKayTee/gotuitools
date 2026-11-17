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
// Built in: charmtone, charmtone-light, terminal. A program registers its
// own with Register; "<name>-light" is the light variant FindFor picks on
// a light terminal (tea.RequestBackgroundColor, BackgroundColorMsg.IsDark).
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
