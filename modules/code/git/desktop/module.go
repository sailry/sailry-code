package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func Register(assembly *native.Assembly) {
	page := Page()
	assembly.RegisterPanelResource(native.PanelResource{
		Kind: page.Resource, Label: page.Feature.Label, Icon: page.Feature.Icon, Order: 40,
		Mods: ui.Ctrl | ui.Shift, Key: ui.KeyG, Render: renderPanel, BindWorkspace: mount,
	})
	assembly.ProjectWorkflows = append(assembly.ProjectWorkflows, func(host native.ProjectWorkflowHost) {
		host.RegisterMenu(native.ProjectMenuAction{Order: 20, Context: func(c *ui.Context, menu *ui.Menu, project *native.ProjectMenuProject) {
			if project.Offline {
				return
			}
			if workspace, ok := host.ProjectWorkspace(project); ok && workspace.Presentation != nil {
				projectMenu(c, menu, workspace.Presentation, workspace.Current)
			}
		}})
	})
	assembly.PluginControlReplacements = append(assembly.PluginControlReplacements,
		native.PluginControlReplacement{Package: "git", Slot: "context", ID: "branch"},
		native.PluginControlReplacement{Package: "git", Slot: "project_menu", ID: "manage"},
		native.PluginControlReplacement{Package: "git", Slot: "status", ID: "changes"},
	)
	page.Feature.Label = pageLabel
	assembly.WorkspacePages = append(assembly.WorkspacePages, page)
	assembly.Locales = append(assembly.Locales, Locales)
}
