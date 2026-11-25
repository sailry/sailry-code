package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func Page() native.NativePage {
	return native.NativePage{
		Page:              native.GitPage,
		Feature:           native.Feature{ID: "page:git", Icon: icons.Network, Label: "git", DefaultPinned: true},
		Usage:             native.UsagePageGit,
		Resource:          native.Changes,
		CheckpointHistory: true,
		Render: func(c *ui.Context, app *native.App) {
			app.RenderResource(c, func(project *native.Project) { render(c, project) })
		},
		RenderDetails: details,
		BindKeys:      func(c *ui.Context, project *native.Project, scope ui.Element) { project.GitKeys(c, scope) },
	}
}

func render(c *ui.Context, project *native.Project) {
	column := ui.Column(c.Key(native.Changes)).Grow(1).MinWidth(0).MinHeight(0)
	width := column.LayoutBounds().W
	column.Children(func() {
		project.Git.Show(project.Git.Change())
		project.ActionDialog(c)
		project.DocumentTabs(c, native.Changes)
		project.GitDiff(c, project.Git.Change(), width)
		project.GitDialogs(c)
	})
}

func details(c *ui.Context, app *native.App, project *native.Project) {
	reveal := app.RevealResourceFile()
	ui.Column(c.Key(native.Changes)).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		project.GitRepository(c, true, false, project.Git.Show, reveal)
	})
}
