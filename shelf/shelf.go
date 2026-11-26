// Package shelf is groups of tiles one above another, each group a
// name over a row of tiles that moves sideways: albums by kind, films
// by genre. A Shelf holds the mark and what is shown and moves them;
// Lines draws the groups from tiles the program draws, with their
// mouse zones.
package shelf

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/layout"
	"github.com/MaximKayTee/gotuitools/text"
)

// Group is a named row of tiles.
type Group struct {
	Name string
	// Keys name its tiles, in order: the mark stays on its key when the
	// groups are set again.
	Keys []string
	// Note stands in the tiles' place while there are none: "Loading",
	// an error.
	Note string
}

type group struct {
	Group
	// cur is the marked tile, off the first one shown.
	cur, off int
}

// Shelf is the groups, the marked one and the first one shown; each
// group keeps its own marked tile. The zero value is an empty shelf.
type Shelf struct {
	groups   []group
	cur, top int
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// Set replaces the groups. A group of a name there was keeps its
// place: the mark on the tile of the same key, else as near as there
// is; the marked group stays the marked one.
func (s *Shelf) Set(groups []Group) {
	old := map[string]group{}
	for _, g := range s.groups {
		old[g.Name] = g
	}
	marked := ""
	if s.cur < len(s.groups) {
		marked = s.groups[s.cur].Name
	}
	s.cur = 0
	next := make([]group, len(groups))
	for i, g := range groups {
		n := group{Group: g}
		if o, ok := old[g.Name]; ok {
			n.cur, n.off = o.cur, o.off
			if o.cur < len(o.Keys) {
				for j, k := range g.Keys {
					if k == o.Keys[o.cur] {
						n.off += j - n.cur
						n.cur = j
						break
					}
				}
			}
			n.cur = clamp(n.cur, 0, len(g.Keys)-1)
			n.off = clamp(n.off, 0, n.cur)
		}
		if g.Name == marked {
			s.cur = i
		}
		next[i] = n
	}
	s.groups = next
	s.top = clamp(s.top, 0, s.cur)
}

// Len is the number of groups.
func (s *Shelf) Len() int { return len(s.groups) }

// Marked is the marked group and its marked tile; false without one.
func (s *Shelf) Marked() (g, i int, ok bool) {
	if s.cur >= len(s.groups) {
		return 0, 0, false
	}
	gr := s.groups[s.cur]
	if gr.cur >= len(gr.Keys) {
		return s.cur, 0, false
	}
	return s.cur, gr.cur, true
}

// Mark marks tile i of group g.
func (s *Shelf) Mark(g, i int) {
	if g < 0 || g >= len(s.groups) {
		return
	}
	s.cur = g
	s.groups[g].cur = clamp(i, 0, len(s.groups[g].Keys)-1)
}

// Across moves the mark d tiles on in the marked group, to its ends at
// most; false when it did not move.
func (s *Shelf) Across(d int) bool {
	if s.cur >= len(s.groups) {
		return false
	}
	g := &s.groups[s.cur]
	c := clamp(g.cur+d, 0, len(g.Keys)-1)
	moved := c != g.cur
	g.cur = c
	return moved
}

// Down moves the mark d groups down, to the first or the last at most;
// false when it did not move.
func (s *Shelf) Down(d int) bool {
	c := clamp(s.cur+d, 0, len(s.groups)-1)
	moved := c != s.cur
	s.cur = c
	return moved
}

// show scrolls so the marked group is among vis groups shown, and each
// group's marked tile among per tiles.
func (s *Shelf) show(vis, per int) {
	vis, per = max(vis, 1), max(per, 1)
	s.top = clamp(s.top, s.cur-vis+1, s.cur)
	s.top = clamp(s.top, 0, max(len(s.groups)-vis, 0))
	for i := range s.groups {
		g := &s.groups[i]
		g.off = clamp(g.off, g.cur-per+1, g.cur)
		g.off = clamp(g.off, 0, max(len(g.Keys)-per, 0))
	}
}

// Look is how a shelf is drawn.
type Look struct {
	// TileW x TileH cells is a tile, the text under its picture and
	// all; Gap the cells between two, 0 is 2.
	TileW, TileH, Gap int
	// Tile draws tile i of group g: TileH lines of TileW cells.
	Tile func(g, i int, marked bool) []string
	// Name draws a group's name, Muted the count beside it, a note and
	// None; nil: as they are.
	Name  func(name string, marked bool) string
	Muted func(s string) string
	// None is said without groups; "" is "None".
	None string
	// Open runs on a click on the marked tile.
	Open func(g, i int) tea.Cmd
}

func (l Look) gap() int {
	if l.Gap <= 0 {
		return 2
	}
	return l.Gap
}

// Per is the tiles of a group shown in w cells; Visible the whole
// groups in h rows: one at least each.
func (l Look) Per(w int) int { return max((w-1+l.gap())/(max(l.TileW, 1)+l.gap()), 1) }

// Visible is the whole groups shown in h rows, one at least.
func (l Look) Visible(h int) int { return max((h+1)/(l.TileH+2), 1) }

func (l Look) muted(s string) string {
	if l.Muted != nil {
		return l.Muted(s)
	}
	return s
}

// Lines draws the shelf at x0, y0 in w x h cells, h lines at most: each
// group its name, with the marked tile's place among more tiles than
// fit, then its tiles in a row and a blank line. Groups past the foot
// are cut, tiles past the right not drawn. The marked group and tile
// are scrolled into what is shown. With zones: a click marks a tile,
// one on the marked tile runs Open; the wheel moves the mark a group.
func (s *Shelf) Lines(l Look, x0, y0, w, h int, zones *layout.Zones) []string {
	if h <= 0 || l.Tile == nil || l.TileW <= 0 || l.TileH <= 0 {
		return nil
	}
	if len(s.groups) == 0 {
		none := l.None
		if none == "" {
			none = "None"
		}
		return []string{"  " + l.muted(none)}
	}
	per := l.Per(w)
	s.show(l.Visible(h), per)
	if zones != nil {
		zones.Add(layout.Zone{Rect: layout.Rect{X: x0, Y: y0, W: w, H: h}, Wheel: func(d int) tea.Cmd {
			s.Down(d)
			return nil
		}})
	}
	lines := make([]string, 0, h)
	gap := text.Spaces(l.gap())
	for gi := s.top; gi < len(s.groups) && len(lines) < h; gi++ {
		g := &s.groups[gi]
		marked := gi == s.cur
		name := g.Name
		if l.Name != nil {
			name = l.Name(name, marked)
		}
		if len(g.Keys) > per {
			name += "  " + l.muted(strconv.Itoa(g.cur+1)+"/"+strconv.Itoa(len(g.Keys)))
		}
		lines = append(lines, " "+name)
		if len(g.Keys) == 0 {
			lines = append(lines, " "+l.muted(text.Truncate(text.OneLine(g.Note), w-2)))
			for y := 1; y < l.TileH+1 && len(lines) < h; y++ {
				lines = append(lines, "")
			}
			continue
		}
		top := y0 + len(lines)
		tiles := make([][]string, 0, per)
		for i := g.off; i < len(g.Keys) && i < g.off+per; i++ {
			tiles = append(tiles, l.Tile(gi, i, marked && i == g.cur))
			if room := y0 + h - top; zones != nil && room > 0 {
				zones.Add(layout.Zone{Rect: layout.Rect{X: x0 + 1 + (i-g.off)*(l.TileW+l.gap()), Y: top, W: l.TileW, H: min(l.TileH, room)},
					Click: func(int, int) tea.Cmd {
						if s.cur == gi && g.cur == i && l.Open != nil {
							return l.Open(gi, i)
						}
						s.cur, g.cur = gi, i
						return nil
					}})
			}
		}
		for y := 0; y < l.TileH && len(lines) < h; y++ {
			var b strings.Builder
			b.WriteString(" ")
			for c, t := range tiles {
				if c > 0 {
					b.WriteString(gap)
				}
				if y < len(t) {
					b.WriteString(text.Fit(t[y], l.TileW))
				} else {
					b.WriteString(text.Spaces(l.TileW))
				}
			}
			lines = append(lines, b.String())
		}
		if len(lines) < h {
			lines = append(lines, "")
		}
	}
	return lines
}
