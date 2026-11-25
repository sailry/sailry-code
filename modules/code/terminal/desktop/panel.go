package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func registerPanel(assembly *native.Assembly) {
	assembly.RegisterPanelResource(native.PanelResource{
		Kind: native.Terminal, Label: "terminal", Icon: icons.Terminal, Order: 20,
		Mods: ui.Ctrl, Key: ui.KeyBackquote, Numbered: true,
		Available: func(state *native.PanelState) bool {
			return !state.Live || state.Worktree && state.TerminalActions != nil
		},
		Open: native.OpenTerminalTab, Render: native.RenderTerminalTab, Find: native.FindTerminalTab,
		ConfirmClose: confirmTabClose, Release: native.ReleaseTerminalTab,
	}, func(host native.PanelResourceHost) {
		if host.BindTerminal != nil {
			host.App.RegisterProjectResourceAction(native.ProjectResourceAction{
				Label: "workspace_new_terminal", Icon: icons.Terminal, Order: 10, Open: host.BindTerminal(),
			})
		}
	})
}

func confirmTabClose(state *native.PanelState, tab native.PanelTab) native.CloseConfirmation {
	if actions := state.TerminalActions; actions != nil && actions.Close != nil {
		return native.CloseConfirmation{Title: "workspace_close_terminal", Close: func() { actions.Close(tab.ID) }}
	}
	return native.CloseConfirmation{}
}
