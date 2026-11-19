package main

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/text"
)

type textTab struct{ m *model }

func (t *textTab) name() string                { return "text" }
func (t *textTab) title() string               { return "Text" }
func (t *textTab) update(tea.Msg) tea.Cmd      { return nil }
func (t *textTab) commands() []command.Command { return nil }
func (t *textTab) status() string {
	return "Values: text.size, text.duration, list.rows, layout.split"
}

func (t *textTab) view(w, _ int) string {
	th, c := t.m.th, t.m.store.Get()
	size, d, rows := int64(c.Text.Size), c.Text.Duration, int64(c.List.Rows)
	now := time.Now()
	path := t.m.store.Path()

	type row struct{ call, result string }
	sections := []struct {
		name string
		rows []row
	}{
		{"Sizes", []row{
			{"Bytes(size)", text.Bytes(size)},
			{"ExactBytes(size)", text.ExactBytes(size)},
			{"Rate(size)", text.Rate(size)},
			{"Compact(size)", text.Compact(size)},
			{"Thousands(size)", text.Thousands(size)},
		}},
		{"Times", []row{
			{"Duration(d)", text.Duration(d)},
			{"Clock(d)", text.Clock(d)},
			{"Rounded(d)", text.Rounded(d)},
			{"Ago(now-d)", text.Ago(now.Add(-d), now)},
			{"Since(now-d)", text.Since(now.Add(-d), now)},
			{"Until(now+d)", text.Until(now.Add(d), now)},
		}},
		{"Counts", []row{
			{"Plural(rows, \"row\")", text.Plural(rows, "row")},
			{"Percent(split)", text.Percent(float64(c.Layout.Split) / 100)},
		}},
		{"Fitting", []row{
			{"Truncate(path, 24)", text.Truncate(path, 24)},
			{"TruncateStart(path, 24)", text.TruncateStart(path, 24)},
			{"Facts(...)", text.Facts(text.Bytes(size), "", text.Duration(d), text.Plural(rows, "row"))},
		}},
	}

	head, call := th.Lip("ui.text.focus"), th.Lip("ui.text.inactive")
	callW := 26
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, head.Render(s.name))
		for _, r := range s.rows {
			lines = append(lines, call.Render(text.Pad(r.call, callW))+text.Truncate(r.result, max(w-callW, 1)))
		}
	}

	wrapW := max(min(w, 60)-callW, 10)
	lines = append(lines, "", head.Render("Wrap(scopes, "+text.Thousands(int64(wrapW))+")"))
	for _, l := range text.Wrap(strings.Join(uiScopes, " "), wrapW) {
		lines = append(lines, text.Spaces(callW)+l)
	}
	return strings.Join(lines, "\n")
}
