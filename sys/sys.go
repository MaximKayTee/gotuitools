// Package sys reaches outside the terminal: the clipboard, desktop
// notifications, other programs, the user's editor.
package sys

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// OSC52 is the clipboard set sequence for text.
func OSC52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x1b\\"
}

// Notification is the desktop notification sequence (OSC 99, kitty's)
// for title with body under it, "" none. app names the program: a
// later notification of the same app replaces the one shown. Text is
// base64, so any is safe.
func Notification(app, title, body string) string {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	id := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, app)
	meta := "e=1"
	if id != "" {
		meta = "i=" + id + ":e=1:f=" + b64(app)
	}
	if body == "" {
		return "\x1b]99;" + meta + ";" + b64(title) + "\x1b\\"
	}
	return "\x1b]99;" + meta + ":d=0;" + b64(title) + "\x1b\\" +
		"\x1b]99;" + meta + ":d=1:p=body;" + b64(body) + "\x1b\\"
}

// Notify shows a desktop notification through the terminal.
func Notify(app, title, body string) tea.Cmd { return tea.Raw(Notification(app, title, body)) }

// Copy sets the clipboard to text through the terminal (OSC 52).
func Copy(text string) tea.Cmd { return tea.SetClipboard(text) }

// ErrNoTool is a missing clipboard program.
var ErrNoTool = errors.New("clipboard program not found")

// CopyData puts data of a MIME type on the clipboard with wl-copy, else
// xclip. Their output is dropped: the forked owner would hold a pipe.
func CopyData(ctx context.Context, mime string, data []byte) error {
	args := []string{"wl-copy", "-t", mime}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		args = []string{"xclip", "-selection", "clipboard", "-t", mime}
	}
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNoTool, args[0])
	}
	cmd := exec.CommandContext(ctx, bin, args[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	return nil
}

// Paste reads the clipboard's first offer whose type starts with
// prefix ("image/", "text/plain"): its data and type. wl-paste under
// Wayland, else xclip.
func Paste(prefix string) (data []byte, mime string, err error) {
	list, get := []string{"wl-paste", "--list-types"}, []string{"wl-paste", "--type"}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		list = []string{"xclip", "-selection", "clipboard", "-o", "-t", "TARGETS"}
		get = []string{"xclip", "-selection", "clipboard", "-o", "-t"}
	}
	if _, err := exec.LookPath(list[0]); err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrNoTool, list[0])
	}
	types, err := exec.Command(list[0], list[1:]...).Output()
	if err != nil {
		return nil, "", fmt.Errorf("clipboard empty (%s: %v)", list[0], err)
	}
	for _, t := range strings.Fields(string(types)) {
		if strings.HasPrefix(t, prefix) {
			data, err := exec.Command(get[0], append(get[1:], t)...).Output()
			return data, t, err
		}
	}
	return nil, "", fmt.Errorf("no %s on the clipboard", strings.TrimSuffix(prefix, "/"))
}

// DroppedPath reads a path a terminal pasted when a file was dropped on
// it: plain, quoted, with escaped spaces, or a file:// URL. ok is false
// when it is not an existing file.
func DroppedPath(text string) (path string, ok bool) {
	path = strings.Trim(strings.TrimSpace(text), `'"`)
	if u, err := url.Parse(path); err == nil && u.Scheme == "file" {
		path = u.Path
	}
	path = strings.ReplaceAll(path, `\ `, " ")
	st, err := os.Stat(path)
	return path, err == nil && st.Mode().IsRegular()
}

// Open starts bin ("" xdg-open) on target, detached. "<bin> not found"
// when it is not on PATH.
func Open(bin, target string) error {
	bin = cmp.Or(bin, "xdg-open")
	abs, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%s not found", bin)
	}
	return Detach(exec.Command(abs, target))
}

// Detach starts cmd in a session of its own, stdio on /dev/null unless
// set, and reaps it.
func Detach(cmd *exec.Cmd) error {
	setsid(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Editor is the user's editor command line: setting, else $VISUAL,
// else $EDITOR, else vi.
func Editor(setting string) string {
	return cmp.Or(strings.TrimSpace(setting), os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
}

// EditorCommand opens file in editor (a command line such as "nvim" or
// "code --reuse-window"; "" is Editor("")), at line when line > 0, in
// the line syntax that editor takes.
func EditorCommand(editor, file string, line int) *exec.Cmd {
	f := strings.Fields(Editor(editor))
	args := []string{file}
	if line > 0 {
		ln := strconv.Itoa(line)
		switch filepath.Base(f[0]) {
		case "hx", "helix", "subl", "zed":
			args = []string{file + ":" + ln}
		case "code", "codium", "code-oss", "cursor":
			args = []string{"--wait", "-g", file + ":" + ln}
		case "kate":
			args = []string{"-l", ln, file}
		default: // vi, vim, nvim, nano, emacs, micro, kak, ...
			args = []string{"+" + ln, file}
		}
	}
	return exec.Command(f[0], append(f[1:], args...)...)
}

// EditedMsg is the end of Edit: Err is nil, a start or exit error, or
// "<editor> not found".
type EditedMsg struct {
	File string
	Err  error
}

// ToModals: it reaches open modals (modal.ToModals).
func (EditedMsg) ToModals() {}

// Edit suspends the program and opens file in the user's editor at line
// (0 the start); EditedMsg follows.
func Edit(editor, file string, line int) tea.Cmd {
	cmd := EditorCommand(editor, file, line)
	if cmd.Err != nil {
		return func() tea.Msg { return EditedMsg{file, fmt.Errorf("%s not found", filepath.Base(cmd.Args[0]))} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if errors.Is(err, exec.ErrNotFound) {
			err = fmt.Errorf("%s not found", filepath.Base(cmd.Args[0]))
		}
		return EditedMsg{file, err}
	})
}
