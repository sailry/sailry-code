package desktop

import "github.com/sailry/sailry-harness/sdk/desktop/native"

func Register(assembly *native.Assembly) {
	assembly.SettingsPages = append(assembly.SettingsPages, Page())
	assembly.Locales = append(assembly.Locales, Locales)
}
