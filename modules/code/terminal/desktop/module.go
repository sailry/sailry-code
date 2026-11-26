package desktop

import "github.com/sailry/sailry-harness/sdk/desktop/native"

func Register(assembly *native.Assembly) {
	assembly.RegisterWorkspacePane(native.TerminalWorkspacePane())
	registerPanel(assembly)
	assembly.PluginControlReplacements = append(assembly.PluginControlReplacements,
		native.PluginControlReplacement{Package: "commands", Slot: "project", ID: "terminal-cli"},
		native.PluginControlReplacement{Package: "commands", Slot: "project_menu", ID: "terminal-menu-new"},
		native.PluginControlReplacement{Package: "commands", Slot: "project_menu", ID: "terminal-menu-cli"},
	)
	assembly.SettingsPages = append(assembly.SettingsPages, Page())
	assembly.Locales = append(assembly.Locales, Locales)
}
