// Package shelf is groups of tiles one above another: albums by kind,
// films by genre. Each group is a name over its tiles, in one row that
// moves sideways or, with Look.Wrap, in as many rows as they take, a
// grid; a group can be folded to its name. A Shelf holds the mark and
// what is shown and moves them; Lines draws the groups from tiles the
// program draws, with their mouse zones.
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
	// Folded: its name alone is drawn, its tiles not; the mark stops on
	// the name.
	Folded bool
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
	// wrap and per are the last draw's: the tiles in rows, so many a
	// row; scroll the lines above what a grid shows.
	wrap   bool
	per    int
	scroll int
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

// Marked is the marked group and its marked tile; false without one,
// and on a folded group: g is the group all the same.
func (s *Shelf) Marked() (g, i int, ok bool) {
	if s.cur >= len(s.groups) {
		return 0, 0, false
	}
	gr := s.groups[s.cur]
	if gr.Folded || gr.cur >= len(gr.Keys) {
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
// in a grid d rows of tiles, on to the next group from a group's last
// row, a folded group one stop. False when it did not move.
func (s *Shelf) Down(d int) bool {
	if s.wrap {
		moved := false
		for ; d > 0 && s.step(1); d-- {
			moved = true
		}
		for ; d < 0 && s.step(-1); d++ {
			moved = true
		}
		return moved
	}
	c := clamp(s.cur+d, 0, len(s.groups)-1)
	moved := c != s.cur
	s.cur = c
	return moved
}

// open reports g drawn with tiles.
func (g *group) open() bool { return !g.Folded && len(g.Keys) > 0 }

// step moves the mark a row of a grid down (dir 1) or up (-1), in its
// column where the row has one.
func (s *Shelf) step(dir int) bool {
	if s.cur >= len(s.groups) {
		return false
	}
	per := max(s.per, 1)
	g := &s.groups[s.cur]
	col := 0
	if g.open() {
		col = g.cur % per
		row, last := g.cur/per, (len(g.Keys)-1)/per
		if dir > 0 && row < last {
			g.cur = min(g.cur+per, len(g.Keys)-1)
			return true
		}
		if dir < 0 && row > 0 {
			g.cur -= per
			return true
		}
	}
	if s.cur+dir < 0 || s.cur+dir >= len(s.groups) {
		return false
	}
	s.cur += dir
	if n := &s.groups[s.cur]; n.open() {
		at := col
		if dir < 0 {
			at += (len(n.Keys) - 1) / per * per
		}
		n.cur = min(at, len(n.Keys)-1)
	}
	return true
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
	// Wrap draws a group's tiles in as many rows as they take, the
	// groups a grid that scrolls by its lines.
	Wrap bool
	// Fold runs on a click on a group's name, for the program to fold
	// the group or open it; with it a group's name has a mark, open or
	// folded.
	Fold func(g int) tea.Cmd
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

// name is group gi's name as drawn: the fold's mark, the name, then the
// count: the marked tile's place among more than fit in a row, or with
// Wrap the number of tiles.
func (s *Shelf) name(l Look, gi, per int) string {
	g := &s.groups[gi]
	name := g.Name
	if l.Name != nil {
		name = l.Name(name, gi == s.cur)
	}
	if l.Fold != nil {
		mark := "▾ "
		if g.Folded {
			mark = "▸ "
		}
		name = l.muted(mark) + name
	}
	switch {
	case len(g.Keys) == 0:
	case l.Wrap || g.Folded:
		name += "  " + l.muted(strconv.Itoa(len(g.Keys)))
	case len(g.Keys) > per:
		name += "  " + l.muted(strconv.Itoa(g.cur+1)+"/"+strconv.Itoa(len(g.Keys)))
	}
	return " " + name
}

// grid draws the groups with their tiles in rows, from line s.scroll of
// all their lines: each group its name, its rows of tiles, a blank
// line; a folded group its name alone. Only tiles of rows shown are
// drawn.
func (s *Shelf) grid(l Look, x0, y0, w, h int, zones *layout.Zones) []string {
	per := l.Per(w)
	s.wrap, s.per = true, per
	rows := func(g *group) int {
		if !g.open() {
			return 0
		}
		return (len(g.Keys) + per - 1) / per
	}
	// the lines of a group: its name, its rows or its note, a blank
	height := func(g *group) int {
		switch {
		case g.Folded:
			return 1
		case len(g.Keys) == 0:
			return 3
		}
		return 1 + rows(g)*l.TileH + 1
	}
	// the marked tile's row, with its group's name when it is the first
	// row, is among the lines shown
	total, from, to := 0, 0, 1
	for gi := range s.groups {
		g := &s.groups[gi]
		if gi == s.cur {
			from, to = total, total+1
			if g.open() {
				row := g.cur / per
				to = total + 1 + (row+1)*l.TileH
				if row > 0 {
					from = total + 1 + row*l.TileH
				}
			}
		}
		total += height(g)
	}
	s.scroll = min(s.scroll, from)
	s.scroll = max(s.scroll, to-h)
	s.scroll = clamp(s.scroll, 0, max(total-h, 0))
	lines := make([]string, 0, h)
	// put adds the line at y of all the lines when it is shown, with a
	// zone when it has one
	put := func(y int, line string) {
		if y >= s.scroll && y < s.scroll+h && len(lines) == y-s.scroll {
			lines = append(lines, line)
		}
	}
	gap := text.Spaces(l.gap())
	y := 0
	for gi := range s.groups {
		g := &s.groups[gi]
		if y >= s.scroll+h {
			break
		}
		if hy := y; hy >= s.scroll && hy < s.scroll+h && zones != nil && l.Fold != nil {
			zones.Add(layout.Zone{Rect: layout.Rect{X: x0, Y: y0 + hy - s.scroll, W: w, H: 1}, Pointer: layout.PointerPointer,
				Click: func(int, int) tea.Cmd { s.cur = gi; return l.Fold(gi) }})
		}
		put(y, s.name(l, gi, per))
		y++
		switch {
		case g.Folded:
			continue
		case len(g.Keys) == 0:
			put(y, " "+l.muted(text.Truncate(text.OneLine(g.Note), w-2)))
			put(y+1, "")
			y += 2
			continue
		}
		for r := range rows(g) {
			if y+l.TileH <= s.scroll || y >= s.scroll+h {
				y += l.TileH
				continue
			}
			first := r * per
			tiles := make([][]string, 0, per)
			for i := first; i < len(g.Keys) && i < first+per; i++ {
				tiles = append(tiles, l.Tile(gi, i, gi == s.cur && i == g.cur))
				top, foot := max(y, s.scroll), min(y+l.TileH, s.scroll+h)
				if zones != nil && foot > top {
					zones.Add(layout.Zone{Rect: layout.Rect{X: x0 + 1 + (i-first)*(l.TileW+l.gap()), Y: y0 + top - s.scroll, W: l.TileW, H: foot - top},
						Click: func(int, int) tea.Cmd {
							if s.cur == gi && g.cur == i && l.Open != nil {
								return l.Open(gi, i)
							}
							s.cur, g.cur = gi, i
							return nil
						}})
				}
			}
			for ty := range l.TileH {
				var b strings.Builder
				b.WriteString(" ")
				for c, t := range tiles {
					if c > 0 {
						b.WriteString(gap)
					}
					if ty < len(t) {
						b.WriteString(text.Fit(t[ty], l.TileW))
					} else {
						b.WriteString(text.Spaces(l.TileW))
					}
				}
				put(y+ty, b.String())
			}
			y += l.TileH
		}
		put(y, "")
		y++
	}
	return lines
}

// Lines draws the shelf at x0, y0 in w x h cells, h lines at most: each
// group its name, with the marked tile's place among more tiles than
// fit, then its tiles in a row and a blank line. Groups past the foot
// are cut, tiles past the right not drawn. With Look.Wrap a group's
// tiles are in as many rows as they take and the lines scroll. The
// marked group and tile are scrolled into what is shown. With zones: a
// click marks a tile, one on the marked tile runs Open, one on a
// group's name Fold; the wheel moves the mark a group, a row with Wrap.
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
	if l.Wrap {
		if zones != nil {
			zones.Add(layout.Zone{Rect: layout.Rect{X: x0, Y: y0, W: w, H: h}, Wheel: func(d int) tea.Cmd {
				s.Down(d)
				return nil
			}})
		}
		return s.grid(l, x0, y0, w, h, zones)
	}
	per := l.Per(w)
	s.wrap, s.per = false, per
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
		if zones != nil && l.Fold != nil {
			zones.Add(layout.Zone{Rect: layout.Rect{X: x0, Y: y0 + len(lines), W: w, H: 1}, Pointer: layout.PointerPointer,
				Click: func(int, int) tea.Cmd { s.cur = gi; return l.Fold(gi) }})
		}
		lines = append(lines, s.name(l, gi, per))
		if g.Folded {
			continue
		}
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
