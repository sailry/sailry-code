package desktop

import "github.com/sailry/sailry-harness/sdk/desktop/native"

func Register(assembly *native.Assembly) {
	page := Page()
	page.Feature.Label = pageLabel
	assembly.WorkspacePages = append(assembly.WorkspacePages, page)
	assembly.Locales = append(assembly.Locales, Locales)
}
