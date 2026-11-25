package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func Register(assembly *native.Assembly) {
	page := Page()
	assembly.RegisterPanelResource(native.PanelResource{
		Kind: page.Resource, Label: page.Feature.Label, Icon: page.Feature.Icon, Order: 40,
		Mods: ui.Ctrl | ui.Shift, Key: ui.KeyG, Render: native.RenderProjectPanelResource,
	})
	page.Feature.Label = pageLabel
	assembly.WorkspacePages = append(assembly.WorkspacePages, page)
	assembly.Locales = append(assembly.Locales, Locales)
}
