package codeindexdesktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/settings"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestCodeIndexPersistenceControl(t *testing.T) {
	assembly := native.Assemble(Register)
	if len(assembly.ExecutionSettings) != 1 || len(assembly.ProjectWorkflows) != 0 {
		t.Fatal("Code index must contribute its execution setting without project workflows")
	}
	state := settings.NewLive()
	state.Open(settings.Conversation)
	binding := assembly.ExecutionSettings[0](settings.ExecutionSettingHost{
		Execute: func(command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
			if command.Kind != "read_code_index_settings" {
				t.Fatalf("read = %q", command.Kind)
			}
			done(nil, protocol.Output{Kind: "code_index_settings", Data: protocol.CodeIndexSettings{Revision: 1, Enabled: true, Persistent: new(false)}}, nil)
		},
		Save: func(command protocol.Command, _ settings.Messages, done func(protocol.Output, bool)) {
			input, ok := command.Data.(protocol.CommandSaveCodeIndexSettingsPayload)
			if command.Kind != "save_code_index_settings" || !ok || input.ExpectedRevision != 1 || input.Settings.Persistent == nil || !*input.Settings.Persistent {
				t.Fatalf("save = %+v", command)
			}
			input.Settings.Revision++
			done(protocol.Output{Kind: "code_index_settings", Data: input.Settings}, true)
		},
	}).(*Setting)
	state.ExecutionSettings = []settings.ExecutionSetting{binding}
	binding.Read()
	tester := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, "", theme.Light)
		settings.Page(c, state, settings.Look{})
	}, 920, 800)
	tester.SetLocale("en")
	tester.Frame()
	toggle, ok := tester.AccessByRole(ui.RoleSwitch, i18n.In("en", "settings_code_index_persistent"))
	if !ok || toggle.Disabled {
		t.Fatal("full code index switch unavailable")
	}
	if _, ok := tester.AccessByRole(ui.RoleMenuButton, "Project"); ok {
		t.Fatal("full code index uses a project selector")
	}
	tester.ClickAt(toggle.Bounds.X+10, toggle.Bounds.Y+10)
	tester.Frame()
	if binding.Persistent == nil || !*binding.Persistent || binding.Revision != 2 {
		t.Fatalf("full code index switch did not save: %+v", binding)
	}
}
