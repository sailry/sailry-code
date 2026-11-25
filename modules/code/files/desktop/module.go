package desktop

import (
	"embed"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

//go:embed locales/*.yml
var locales embed.FS

func Register(assembly *native.Assembly) {
	registerLinks(assembly)
	page := Page()
	assembly.RegisterPanelResource(native.PanelResource{
		Kind: native.Files, Label: "files", Icon: page.Feature.Icon, Order: 30,
		Mods: ui.Cmd, Key: ui.KeyP,
		Render: func(c *ui.Context, state *native.PanelState, tab native.PanelTab) {
			native.RenderFilePanelResource(c, state, tab, renderTree)
		},
	})
	assembly.WorkspacePages = append(assembly.WorkspacePages, page)
	assembly.Locales = append(assembly.Locales, locales)
}
