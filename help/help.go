// Package help is the key hints in a status bar, the keys screen, and
// KEYS.md, all from the same groups of rows.
package help

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

// Hint is a key and what it does, for the status bar.
type Hint struct{ Key, Label string }

// Row is one line of the keys screen: "Up, Down, j, k" | "Move".
type Row struct{ Keys, Action string }

// Group is a titled section of rows.
type Group struct {
	Name string
	Rows []Row
}

// Bar is a status line w cells wide: left, then as many hints as fit,
// flush right, dropped from the end. Styles: ui.statusline,
// ui.statusline.key.
func Bar(th theme.Theme, left string, hints []Hint, w int) string {
	if w <= 0 {
		return ""
	}
	lw := ansi.StringWidth(left)
	var right string
	rw := 0
	key, label := th.Style("ui.statusline.key"), th.Style("ui.statusline")
	for _, h := range hints {
		n := ansi.StringWidth(h.Key) + 1 + ansi.StringWidth(h.Label) + 2
		if 1+min(lw, 12)+rw+n+1 > w {
			break
		}
		right += label.Render("  ") + key.Render(h.Key) + label.Render(" "+h.Label)
		rw += n
	}
	bar := label.Render(" ") + text.Fit(left, w-rw-2) + right + label.Render(" ")
	return text.On(bar, th.Bg("ui.statusline"))
}

// Hints are the given commands' first keys in mode and titles; commands
// without keys are left out.
func Hints(reg *command.Registry, keys *command.Keys, mode string, names ...string) []Hint {
	var out []Hint
	for _, n := range names {
		c, ok := reg.Get(n)
		k := keys.For(mode, n)
		if !ok || k == "" {
			continue
		}
		out = append(out, Hint{Key: k, Label: c.Label()})
	}
	return out
}

// FromCommands groups reg's commands with keys in mode by Command.Group,
// in the order first seen; a row's keys are every binding, shown with
// KeyName.
func FromCommands(reg *command.Registry, keys *command.Keys, mode string) []Group {
	var groups []Group
	at := map[string]int{}
	for _, c := range reg.All() {
		bound := keys.All(mode, c.Name)
		if len(bound) == 0 {
			continue
		}
		names := make([]string, len(bound))
		for i, b := range bound {
			names[i] = KeyName(b)
		}
		i, ok := at[c.Group]
		if !ok {
			i = len(groups)
			at[c.Group] = i
			groups = append(groups, Group{Name: c.Group})
		}
		groups[i].Rows = append(groups[i].Rows, Row{Keys: strings.Join(names, ", "), Action: c.Label()})
	}
	return groups
}

var keyWords = map[string]string{
	"pgup": "PgUp", "pgdown": "PgDn", "esc": "Esc", "enter": "Enter", "tab": "Tab",
	"backspace": "Backspace", "delete": "Delete", "space": "Space", "home": "Home", "end": "End",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "insert": "Insert",
	",": "Comma",
}

// KeyName is a binding as written in docs: "ctrl+p" is "Ctrl+P",
// "g g" is "g g", "f1" is "F1", "shift+tab" is "Shift+Tab".
func KeyName(binding string) string {
	keys := strings.Fields(binding)
	for i, k := range keys {
		if k == "+" {
			continue
		}
		parts := strings.Split(k, "+")
		if k == "+" || strings.HasSuffix(k, "++") {
			parts = append(strings.Split(strings.TrimSuffix(k, "++"), "+"), "+")
		}
		for j, p := range parts {
			last := j == len(parts)-1
			switch {
			case keyWords[p] != "":
				parts[j] = keyWords[p]
			case len(p) >= 2 && p[0] == 'f' && unicode.IsDigit(rune(p[1])):
				parts[j] = strings.ToUpper(p)
			case last && len(parts) > 1 && len(p) == 1:
				parts[j] = strings.ToUpper(p)
			case !last:
				parts[j] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		keys[i] = strings.Join(parts, "+")
	}
	return strings.Join(keys, " ")
}

// Markdown is KEYS.md: a title, intro paragraphs, then each group as a
// "## Name" table of Key | Action.
func Markdown(title, intro string, groups []Group) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if intro = strings.TrimSpace(intro); intro != "" {
		b.WriteString(intro + "\n\n")
	}
	for i, g := range groups {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s\n\n| Key | Action |\n|---|---|\n", g.Name)
		for _, r := range g.Rows {
			fmt.Fprintf(&b, "| %s | %s |\n", r.Keys, r.Action)
		}
	}
	return b.String()
}

// Check compares a KEYS.md with groups: every group heading and row is
// in it, and it has no other rows. Rows of tables headed by a column
// other than Key are not counted. Returns the differences.
func Check(md string, groups []Group) []string {
	var probs []string
	want := 0
	for _, g := range groups {
		if !strings.Contains(md, "## "+g.Name+"\n") {
			probs = append(probs, fmt.Sprintf("no section %q", g.Name))
		}
		for _, r := range g.Rows {
			want++
			if !strings.Contains(md, "| "+r.Keys+" | "+r.Action+" |\n") {
				probs = append(probs, fmt.Sprintf("no row %q: %q", r.Keys, r.Action))
			}
		}
	}
	got, counting := 0, false
	for _, l := range strings.Split(md, "\n") {
		switch {
		case strings.HasPrefix(l, "| Key "):
			counting = true
		case strings.HasPrefix(l, "|---"):
		case strings.HasPrefix(l, "| "):
			if counting {
				got++
			}
		default:
			counting = false
		}
	}
	if got != want {
		probs = append(probs, fmt.Sprintf("%d rows in the file, %d in the keys screen", got, want))
	}
	return slices.Clip(probs)
}
