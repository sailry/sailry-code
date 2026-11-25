package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func Page() native.NativePage {
	return native.NativePage{
		Page: native.FilesPage,
		Feature: native.Feature{
			ID: string(native.FilesPage), Icon: native.FilesIcon, Label: "files_page", DefaultPinned: true, LegacyPackage: "files",
		},
		Usage:               native.UsagePageFiles,
		Resource:            native.Files,
		ReplacesLegacyViews: true,
		LegacyWorkspace:     "files-workspace",
		Render: func(c *ui.Context, app *native.App) {
			app.RenderResource(c, func(project *native.Project) {
				ui.Column(c.Key(native.Files)).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
					project.Files.Show(project.Files.File())
					project.ActionDialog(c)
					project.DocumentTabs(c, native.Files)
					project.FileEditor(c, project.Files.File(), true)
				})
			})
		},
		RenderDetails: func(c *ui.Context, _ *native.App, project *native.Project) {
			ui.Column(c.Key(native.Files)).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
				renderTree(project, c, true, project.Files.Show)
			})
		},
	}
}
