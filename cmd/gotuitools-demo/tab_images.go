package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/termimg"
	"github.com/MaximKayTee/gotuitools/text"
)

const (
	imagePx     = 256
	placementID = 1
)

// imagesTab draws a generated picture through the encoder images.mode
// picks, as is and through termimg.Circle.
type imagesTab struct {
	m          *model
	enc        termimg.Encoder
	mode       string // setting the encoder was chosen for
	resolved   string // mode it resolved to
	q          *termimg.Queue
	pics       []picture
	cols, rows int
}

type picture struct {
	title string
	img   image.Image
	id    uint32
}

func newImagesTab(m *model) *imagesTab {
	t := &imagesTab{m: m, q: termimg.NewQueue(m.opts.out)}
	ids := termimg.IDs{Lo: 1, Hi: 0xFFFF00}
	g := gradient(imagePx)
	t.pics = []picture{{title: "Pack", img: g, id: ids.Take()}, {title: "Circle", img: termimg.Circle(g), id: ids.Take()}}
	return t
}

func (t *imagesTab) name() string  { return "images" }
func (t *imagesTab) title() string { return "Images" }

func (t *imagesTab) commands() []command.Command {
	return []command.Command{
		{Name: "images.mode", Title: "Next mode", Key: "m", Run: do(func() {
			modes := termimg.Modes
			t.m.set("images.mode", modes[(slices.Index(modes, t.m.store.Get().Images.Mode)+1)%len(modes)])
		})},
	}
}

func (t *imagesTab) status() string {
	c := t.m.opts.cell
	mode := t.resolved
	if t.mode == "auto" {
		mode = "auto: " + mode
	}
	return text.Facts(mode, "kitty.Supported "+strconv.FormatBool(t.m.opts.graphics),
		fmt.Sprintf("cell %dx%d px", c.X, c.Y), fmt.Sprintf("%dx%d cells", t.cols, t.rows))
}

// sync follows the images.mode setting and the body height: a new
// encoder gets the pictures again. The Cmd writes what it queued.
func (t *imagesTab) sync() tea.Cmd {
	if mode := t.m.store.Get().Images.Mode; t.enc == nil || mode != t.mode {
		for _, p := range t.pics {
			if t.enc != nil {
				t.q.Push(t.enc.Delete(p.id))
			}
		}
		t.enc, t.resolved = termimg.Choose(mode, t.m.opts.graphics)
		t.mode = mode
		for _, p := range t.pics {
			t.q.Push(t.enc.TransmitImage(p.id, termimg.Scale(p.img, p.img.Bounds(), imagePx, imagePx)))
		}
		t.cols, t.rows = 0, 0
	}
	cols, rows := termimg.Fit(imagePx, imagePx, 32, max(t.m.bodyHeight()-2, 1), termimg.Cell(t.m.opts.cell), true)
	if cols != t.cols || rows != t.rows {
		t.cols, t.rows = cols, rows
		for _, p := range t.pics {
			t.q.Push(t.enc.VirtualPlace(p.id, placementID, cols, rows))
		}
	}
	return t.q.Flush()
}

func (t *imagesTab) update(tea.Msg) tea.Cmd { return nil }

func (t *imagesTab) view(int, int) string {
	th := t.m.th
	lines := make([]string, 0, t.rows+2)
	var head string
	for _, p := range t.pics {
		head += th.Render("ui.text.focus", text.Pad(p.title, t.cols+2))
	}
	lines = append(lines, head, "")
	for r := 0; r < t.rows; r++ {
		var row string
		for _, p := range t.pics {
			row += t.enc.PlaceholderRow(p.id, placementID, r, t.cols) + "  "
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

// gradient is an n x n picture: hue across, lightness down.
func gradient(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		l := 0.85 - 0.6*float64(y)/float64(n-1)
		for x := range n {
			img.Set(x, y, hsl(360*float64(x)/float64(n), 0.7, l))
		}
	}
	return img
}

func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g = c, x
	case hp < 2:
		r, g = x, c
	case hp < 3:
		g, b = c, x
	case hp < 4:
		g, b = x, c
	case hp < 5:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := l - c/2
	to := func(v float64) uint8 { return uint8(math.Round((v + m) * 255)) }
	return color.RGBA{to(r), to(g), to(b), 255}
}
