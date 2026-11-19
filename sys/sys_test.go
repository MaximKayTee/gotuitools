package sys

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEditorCommand(t *testing.T) {
	for _, c := range []struct {
		editor string
		line   int
		want   []string
	}{
		{"nvim", 12, []string{"nvim", "+12", "f.go"}},
		{"hx", 3, []string{"hx", "f.go:3"}},
		{"code --reuse-window", 3, []string{"code", "--reuse-window", "--wait", "-g", "f.go:3"}},
		{"kate", 3, []string{"kate", "-l", "3", "f.go"}},
		{"nvim", 0, []string{"nvim", "f.go"}},
	} {
		if got := EditorCommand(c.editor, "f.go", c.line).Args; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %q", c.editor, got)
		}
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano")
	if Editor("") != "nano" || Editor(" micro ") != "micro" {
		t.Error("Editor")
	}
	t.Setenv("EDITOR", "")
	if Editor("") != "vi" {
		t.Error("vi fallback")
	}
}

func TestDroppedPath(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a b.png")
	os.WriteFile(f, []byte("x"), 0o644)
	for _, in := range []string{f, "'" + f + "'", filepath.Join(dir, `a\ b.png`), "file://" + f} {
		if p, ok := DroppedPath(in); !ok || p != f {
			t.Errorf("%q: %q %v", in, p, ok)
		}
	}
	if _, ok := DroppedPath(dir); ok {
		t.Error("directory accepted")
	}
}

func TestOSC52(t *testing.T) {
	if OSC52("hi") != "\x1b]52;c;aGk=\x1b\\" {
		t.Error(OSC52("hi"))
	}
}

func TestOpenMissing(t *testing.T) {
	if err := Open("no-such-opener-xyz", "x"); err == nil || err.Error() != "no-such-opener-xyz not found" {
		t.Error(err)
	}
}

func TestNotification(t *testing.T) {
	for _, tc := range []struct{ app, title, body, want string }{
		{"gotube", "Copied", "", "\x1b]99;i=gotube:e=1:f=Z290dWJl;Q29waWVk\x1b\\"},
		{"gotube", "Subscribed", "A; B\x1b", "\x1b]99;i=gotube:e=1:f=Z290dWJl:d=0;U3Vic2NyaWJlZA==\x1b\\" +
			"\x1b]99;i=gotube:e=1:f=Z290dWJl:d=1:p=body;QTsgQhs=\x1b\\"},
		{"", "Done", "", "\x1b]99;e=1;RG9uZQ==\x1b\\"},
		{"my app:1", "Done", "", "\x1b]99;i=myapp1:e=1:f=bXkgYXBwOjE=;RG9uZQ==\x1b\\"},
	} {
		if got := Notification(tc.app, tc.title, tc.body); got != tc.want {
			t.Errorf("Notification(%q, %q, %q) = %q, want %q", tc.app, tc.title, tc.body, got, tc.want)
		}
	}
}
