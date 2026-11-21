// Command gotuitools-demo shows the gotuitools components, one tab each.
package main

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/MaximKayTee/gotuitools/command"
	"github.com/MaximKayTee/gotuitools/guide"
	"github.com/MaximKayTee/gotuitools/help"
	"github.com/MaximKayTee/gotuitools/kitty"
	"github.com/MaximKayTee/gotuitools/layout"
	"github.com/MaximKayTee/gotuitools/modal"
	"github.com/MaximKayTee/gotuitools/settings"
	"github.com/MaximKayTee/gotuitools/settingsui"
	"github.com/MaximKayTee/gotuitools/sys"
	"github.com/MaximKayTee/gotuitools/termout"
	"github.com/MaximKayTee/gotuitools/text"
	"github.com/MaximKayTee/gotuitools/theme"
)

const (
	headerHeight = 2
	minW, minH   = 40, 12
)

// tab is one page of the demo.
type tab interface {
	name() string // command and key mode prefix
	title() string
	status() string // left of the status bar
	commands() []command.Command
	update(msg tea.Msg) tea.Cmd
	view(w, h int) string
}

// capturer is a tab that takes every key while it has a line focused.
type capturer interface{ capturing() bool }

// scroller is a tab that moves its own cursor when the page fits.
type scroller interface{ scrollBy(n int) }

// options are what main finds out about the terminal.
type options struct {
	graphics bool
	cell     image.Point
	out      io.Writer // image escapes; nil os.Stdout
}

type model struct {
	tabs   []tab
	scroll []int
	active int
	width  int
	height int

	opts   options
	store  *settings.Store[config]
	th     theme.Theme
	dark   bool
	status string // last error or note

	reg     *command.Registry
	keys    *command.Keys
	disp    command.Dispatcher
	globals []command.Command
	tabCmds map[string][]command.Command

	stack  *modal.Stack
	zones  layout.Zones
	images *imagesTab
	guide  *guide.Guide
}

func newModel(store *settings.Store[config], opts options) *model {
	m := &model{opts: opts, store: store, dark: true}
	m.reg, m.keys, m.stack = &command.Registry{}, &command.Keys{}, modal.NewStack()
	m.disp = command.Dispatcher{Keys: m.keys, Registry: m.reg}
	m.th = theme.Default()
	m.tabs = []tab{
		&themeTab{m: m},
		&textTab{m: m},
		newInputTab(m),
		&listTab{m: m},
		newFuzzyTab(m),
		&modalTab{m: m},
		&layoutTab{m: m},
		&keysTab{m: m},
		&settingsTab{m: m},
	}
	m.images = newImagesTab(m)
	m.tabs = append(m.tabs, m.images)
	m.scroll = make([]int, len(m.tabs))
	m.globals = m.globalCommands()
	m.reg.MustAdd(m.globals...)
	m.tabCmds = map[string][]command.Command{}
	for _, t := range m.tabs {
		m.tabCmds[t.name()] = m.tabCommands(t)
		m.reg.MustAdd(m.tabCmds[t.name()]...)
	}
	m.newGuide()
	m.store.Keys("keys", m.keyModes)
	if err := m.store.Fill(); err != nil {
		m.status = err.Error()
	}
	m.apply()
	return m
}

func (m *model) mode() string { return m.tabs[m.active].name() }

func (m *model) bodyHeight() int { return max(m.height-headerHeight-1, 1) }

// excess is how many lines the active tab is taller than the window.
func (m *model) excess() int {
	h := lipgloss.Height(m.tabs[m.active].view(m.width, m.bodyHeight()))
	return max(h-m.bodyHeight(), 0)
}

// scrollBy scrolls the active tab when it is taller than the window.
func (m *model) scrollBy(n int) bool {
	ex := m.excess()
	if ex == 0 {
		return false
	}
	m.scroll[m.active] = min(max(m.scroll[m.active]+n, 0), ex)
	return true
}

// Init shows the guide by itself on the first start.
func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor, m.images.sync()}
	if m.guide.First() {
		cmds = append(cmds, m.openGuide())
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	stackCmd, handled := m.stack.Update(msg)
	if handled {
		return m, tea.Batch(stackCmd, m.images.sync())
	}
	cmd := m.update(msg)
	return m, tea.Batch(stackCmd, cmd, m.images.sync())
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	t := m.tabs[m.active]
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.status = ""
		if c, ok := t.(capturer); ok && c.capturing() && msg.String() != "ctrl+c" {
			return t.update(msg)
		}
		if cmd, ok := m.disp.Key(m.mode(), msg); ok {
			return cmd
		}
		return t.update(msg)
	case tea.PasteMsg:
		return t.update(msg)

	case settingsui.ChangedMsg, settingsui.ReloadedMsg, sys.EditedMsg:
		if _, ok := msg.(sys.EditedMsg); ok {
			if _, err := m.store.Reload(); err != nil {
				m.status = err.Error()
			}
		}
		m.apply()
		return nil

	case command.ErrorMsg:
		m.status = msg.Error()
		return nil

	case tea.MouseClickMsg:
		if cmd, ok := m.zones.Update(msg); ok {
			return cmd
		}
		if msg.Y < headerHeight {
			return nil
		}
		msg.Y -= headerHeight
		return t.update(msg)
	case tea.MouseMotionMsg:
		cmd, _ := m.zones.Update(msg)
		msg.Y -= headerHeight
		return tea.Batch(cmd, t.update(msg))
	case tea.MouseReleaseMsg:
		msg.Y -= headerHeight
		return t.update(msg)
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.scrollBy(-3) {
				return nil
			}
		case tea.MouseWheelDown:
			if m.scrollBy(3) {
				return nil
			}
		}
		msg.Y -= headerHeight
		return t.update(msg)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.apply()
	}

	var cmds []tea.Cmd
	for _, t := range m.tabs {
		cmds = append(cmds, t.update(msg))
	}
	return tea.Batch(cmds...)
}

// tabLabels are full titles, or numbers for inactive tabs when the full
// bar is wider than the screen.
func (m *model) tabLabels() []string {
	tabSt := m.th.Lip("ui.text.inactive").Padding(0, 1)
	activeSt := m.th.Lip("ui.selection").Padding(0, 1)
	render := func(short bool) ([]string, int) {
		labels := make([]string, len(m.tabs))
		total := 0
		for i, t := range m.tabs {
			st, label := tabSt, fmt.Sprintf("%d %s", (i+1)%10, t.title())
			if i == m.active {
				st = activeSt
			} else if short {
				label = fmt.Sprint((i + 1) % 10)
			}
			labels[i] = st.Render(label)
			total += text.Width(labels[i])
		}
		return labels, total
	}
	labels, total := render(false)
	if m.width > 0 && total > m.width {
		labels, _ = render(true)
	}
	return labels
}

// tabBar draws the tab labels and makes each a click zone.
func (m *model) tabBar() string {
	labels := m.tabLabels()
	m.zones.Reset()
	x := 0
	for i, l := range labels {
		w := text.Width(l)
		m.zones.Add(layout.Zone{Rect: layout.Rect{X: x, Y: 0, W: w, H: 1}, Pointer: layout.PointerPointer,
			Click: func(int, int) tea.Cmd { m.active = i; return nil }})
		x += w
	}
	return text.Truncate(strings.Join(labels, ""), m.width)
}

// statusBar is the active tab's status and key hints.
func (m *model) statusBar(scrollable bool) string {
	t := m.tabs[m.active]
	left := t.status()
	if m.status != "" {
		left = m.th.Render("error", m.status)
	}
	var names []string
	if scrollable {
		names = append(names, "scroll.down")
	}
	for _, c := range m.tabCmds[t.name()] {
		if c.Can() {
			names = append(names, c.Name)
		}
	}
	names = append(names, "tab.next", "keys", "quit")
	return help.Bar(m.th, left, help.Hints(m.reg, m.keys, t.name(), names...), m.width)
}

func (m *model) View() tea.View {
	var screen string
	if small, ok := layout.TooSmall(m.width, m.height, minW, minH); ok {
		screen = small
	} else {
		bar := m.tabBar()
		bodyH := m.bodyHeight()
		body := m.tabs[m.active].view(m.width, bodyH)
		scrollable := false
		lines := strings.Split(body, "\n")
		if len(lines) > bodyH {
			off := min(m.scroll[m.active], len(lines)-bodyH)
			lines = lines[off : off+bodyH]
			scrollable = true
		}
		for i, l := range lines {
			lines[i] = text.Cut(l, m.width)
		}
		body = strings.Join(lines, "\n")
		body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
		screen = m.stack.View(bar+"\n\n"+body+"\n"+m.statusBar(scrollable), m.width)
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = "gotuitools"
	if fg, bg := m.th.Ground(); bg != nil {
		v.BackgroundColor = bg
		v.ForegroundColor = fg
	}
	return v
}

func main() {
	path, err := configPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	store, err := openConfig(path)
	var loadErr *settings.LoadError
	if store == nil || (err != nil && !errors.As(err, &loadErr)) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	out := termout.New(os.Stdout)
	opts := options{graphics: kitty.Supported(os.Getenv), cell: image.Pt(10, 20), out: out}
	if w, h, err := kitty.CellSize(os.Stdout); err == nil {
		opts.cell = image.Pt(w, h)
	}
	m := newModel(store, opts)
	if err != nil {
		m.status = err.Error()
	}

	// images wait for the alternate screen: kitty keeps them per screen
	out.Hold()
	popts := []tea.ProgramOption{tea.WithOutput(out.Renderer())}
	if opts.graphics {
		// image ids are 24-bit SGR colours
		popts = append(popts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	_, err = tea.NewProgram(m, popts...).Run()
	if opts.graphics {
		_, _ = io.WriteString(os.Stdout, kitty.DeleteAll())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
