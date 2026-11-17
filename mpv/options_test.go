package mpv

import (
	"strings"
	"testing"
	"time"
)

func TestOptionListHeaders(t *testing.T) {
	opts := optionList(Source{URL: "https://x", Headers: []string{"User-Agent: Mozilla (KHTML, like Gecko)", "Referer: https://y"}}, Options{})
	got := map[string]string{}
	for _, kv := range opts {
		got[kv[0]] = kv[1]
	}
	if got["user-agent"] != "Mozilla (KHTML, like Gecko)" {
		t.Errorf("user-agent %q", got["user-agent"])
	}
	if got["http-header-fields"] != "Referer: https://y" {
		t.Errorf("fields %q", got["http-header-fields"])
	}
	if got["ytdl"] != "no" {
		t.Error("ytdl", got["ytdl"])
	}
	opts = optionList(Source{Path: "/a"}, Options{YTDL: true, YTDLFormat: "18", Start: 201500 * time.Millisecond})
	s := ""
	for _, kv := range opts {
		s += kv[0] + "=" + kv[1] + ";"
	}
	if !strings.Contains(s, "ytdl=yes;") || !strings.Contains(s, "ytdl-format=18;") || !strings.Contains(s, "start=201.500;") {
		t.Error(s)
	}
}
