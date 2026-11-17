package mpv

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"sync"
)

// ytdlWait holds mpv's load until YouTube's stream URLs answer: they
// 403 until some seconds after yt-dlp's extraction, and ytdl_hook does
// not wait. mpv 0.41 and older only.
//
//go:embed ytdl-wait.lua
var ytdlWait []byte

var ytdlWaitPath = sync.OnceValues(func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "gotuitools", "mpv", "ytdl-wait.lua")
	if b, err := os.ReadFile(path); err == nil && bytes.Equal(b, ytdlWait) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, ytdlWait, 0o644)
})

// YTDLWaitScript is the path of the ytdl-wait script, written to the
// user cache directory on first use: an mpv --script value.
func YTDLWaitScript() (string, error) { return ytdlWaitPath() }
