// Package input is a one-line text editor: the text, a cursor, and the
// keys that edit them.
//
//	left right, home ctrl+a, end ctrl+e   move
//	alt+left alt+b, alt+right alt+f       move a word
//	backspace ctrl+h, delete ctrl+d       delete a character
//	ctrl+w alt+backspace, alt+d           delete a word
//	ctrl+u, ctrl+k                        delete to start, to end
//	paste, printable text                 insert; control characters become spaces
package input

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Line is the text being typed and the cursor in it, in runes.
type Line struct {
	text []rune
	at   int
	// Limit is the most runes the line takes; 0 no limit.
	Limit int
	// Mask, when set, is drawn in place of every character: a secret.
	Mask rune
}

// Of is a line holding text, cursor at the end.
func Of(text string) Line {
	r := []rune(clean(text))
	return Line{text: r, at: len(r)}
}

func (l *Line) String() string { return string(l.text) }

// Cursor is the cursor's index in runes.
func (l *Line) Cursor() int { return l.at }

// Set replaces the text, cursor at the end.
func (l *Line) Set(text string) {
	l.text = []rune(clean(text))
	l.at = len(l.text)
	l.limit()
}

// Insert types text at the cursor.
func (l *Line) Insert(text string) {
	typed := []rune(clean(text))
	l.text = append(l.text[:l.at], append(typed, l.text[l.at:]...)...)
	l.at += len(typed)
	l.limit()
}

func (l *Line) limit() {
	if l.Limit > 0 && len(l.text) > l.Limit {
		l.text = l.text[:l.Limit]
		l.at = min(l.at, l.Limit)
	}
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

// Update edits the line for a key press or paste and reports whether
// the message was for it.
func (l *Line) Update(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		l.Insert(msg.Content)
		return true
	case tea.KeyPressMsg:
		return l.Key(msg)
	}
	return false
}

// Key edits the line and reports whether the key was for it.
func (l *Line) Key(key tea.KeyPressMsg) bool {
	switch key.String() {
	case "left", "ctrl+b":
		l.at = max(l.at-1, 0)
	case "right", "ctrl+f":
		l.at = min(l.at+1, len(l.text))
	case "home", "ctrl+a":
		l.at = 0
	case "end", "ctrl+e":
		l.at = len(l.text)
	case "alt+left", "alt+b", "ctrl+left":
		l.at = l.wordLeft()
	case "alt+right", "alt+f", "ctrl+right":
		l.at = l.wordRight()
	case "backspace", "ctrl+h":
		if l.at > 0 {
			l.text = append(l.text[:l.at-1], l.text[l.at:]...)
			l.at--
		}
	case "delete", "ctrl+d":
		if l.at < len(l.text) {
			l.text = append(l.text[:l.at], l.text[l.at+1:]...)
		}
	case "ctrl+w", "alt+backspace":
		from := l.wordLeft()
		l.text = append(l.text[:from], l.text[l.at:]...)
		l.at = from
	case "alt+d", "alt+delete":
		to := l.wordRight()
		l.text = append(l.text[:l.at], l.text[to:]...)
	case "ctrl+u":
		l.text = append([]rune(nil), l.text[l.at:]...)
		l.at = 0
	case "ctrl+k":
		l.text = l.text[:l.at]
	case "space":
		l.Insert(" ")
	default:
		if key.Text == "" {
			return false
		}
		l.Insert(key.Text)
	}
	return true
}

func word(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (l *Line) wordLeft() int {
	i := l.at
	for i > 0 && !word(l.text[i-1]) {
		i--
	}
	for i > 0 && word(l.text[i-1]) {
		i--
	}
	return i
}

func (l *Line) wordRight() int {
	i := l.at
	for i < len(l.text) && !word(l.text[i]) {
		i++
	}
	for i < len(l.text) && word(l.text[i]) {
		i++
	}
	return i
}

// View is the line in w cells with the cursor drawn in cursor style,
// scrolled so the cursor shows. x is the cursor's cell in the result,
// for a real terminal cursor (tea.NewCursor).
func (l *Line) View(w int, text, cursor lipgloss.Style) (view string, x int) {
	if w <= 0 {
		return "", 0
	}
	shown := l.text
	if l.Mask != 0 {
		shown = []rune(strings.Repeat(string(l.Mask), len(l.text)))
	}
	before := string(shown[:l.at])
	under, after := " ", ""
	if l.at < len(shown) {
		under, after = string(shown[l.at]), string(shown[l.at+1:])
	}
	bw, uw := ansi.StringWidth(before), ansi.StringWidth(under)
	if over := bw + uw - w; over > 0 {
		before = ansi.TruncateLeft(before, over, "")
		bw = ansi.StringWidth(before)
	}
	rest := ansi.Truncate(after, max(w-bw-uw, 0), "")
	view = text.Render(before) + cursor.Render(under) + text.Render(rest)
	if pad := w - bw - uw - ansi.StringWidth(rest); pad > 0 {
		view += text.Render(strings.Repeat(" ", pad))
	}
	return view, bw
}
