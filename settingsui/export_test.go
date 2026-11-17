package settingsui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/MaximKayTee/gotuitools/sys"
)

func sysEdited(path string) tea.Msg { return sys.EditedMsg{File: path} }
