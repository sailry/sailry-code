package desktop

import (
	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func renderPanel(c *ui.Context, state *native.PanelState, tab native.PanelTab) {
	project := state.GitProject()
	if project == nil {
		kit.Result(c, kit.ResultProps{Icon: icons.Network, Title: i18n.T(c, "resource_project_empty")}).Grow(1).Justify(ui.Center)
		return
	}
	pr := viewOf(project)
	box := ui.Column(c.Key("git")).Grow(1).MinHeight(0)
	pr.GitKeys(c, box)
	owns := state.OwnsResource(tab)
	files := state.RegisteredResource(native.Files)
	var reveal func(string)
	if files {
		reveal = func(path string) { state.Show(native.PanelTab{Kind: native.Files, ID: path}) }
	}
	box.Children(func() {
		native.WorkspacePair(c, "changes", &pr.view.side, gitLeast,
			func(narrow bool) { pr.renderDiff(c, tab.ID, narrow) },
			func(stacked bool) {
				pr.GitRepository(c, owns, stacked, func(path string) { state.Show(native.PanelTab{Kind: native.Changes, ID: path}) }, reveal)
			})
	})
	if owns {
		pr.GitDialogs(c)
	}
}
