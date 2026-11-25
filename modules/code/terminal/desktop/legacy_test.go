package desktop_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func checkLegacyTerminalPackage(w *support.Window, project string) {
	w.T.Helper()
	const probe = "Legacy package fixture"
	nativeMenu := w.ProjectMenu(project)
	w.Tester.CloseMenu()
	terminal := slices.Index(nativeMenu, support.Label("workspace_new_terminal"))
	if terminal < 0 {
		w.T.Fatalf("native menu is missing the terminal action: %q", nativeMenu)
	}
	source := w.T.TempDir()
	manifest := `{"$schema":"` + sdktest.ManifestSchema + `","name":"commands","version":"0.1.0","extensions":{"dev.sailry.platform":{
  "api_version":"v1","scope":"host","requires_modules":["terminal"],"actions":["terminals.read","terminals.control"],
  "ui":[
    {"id":"terminal-new","slot":"project","kind":"button","label":{"label":"Legacy project terminal"},"handler":"create"},
    {"id":"terminal-menu-new","slot":"project_menu","kind":"button","order":10,"label":{"label":"Legacy menu terminal"},"handler":"create"},
    {"id":"fixture-probe","slot":"project_menu","kind":"button","order":25,"label":{"label":"` + probe + `"},"handler":"probe"}
  ],"desktop":{"entry":"dev.sailry.platform/terminal.js","ui_entry":"dev.sailry.platform/terminal.js","resources":["dev.sailry.platform/terminal.js"],"renderers":[{"resource":"terminal","create":"terminal-new"}]}
}}}`
	script := `import {View, div} from 'gpui-kit';
import {publishContributions} from 'sailry/sdk';
export default class Fixture extends View {
  init() {
    try { publishContributions([{id:'terminal-menu-new',enabled:true}]); } catch (_) {}
    publishContributions([{id:'fixture-probe',enabled:true}]);
  }
  render() { return div(); }
}`
	if err := os.Mkdir(filepath.Join(source, "dev.sailry.platform"), 0o700); err != nil {
		w.T.Fatal(err)
	}
	for name, data := range map[string]string{"plugin.json": manifest, "dev.sailry.platform/terminal.js": script} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0o600); err != nil {
			w.T.Fatal(err)
		}
	}
	// A synthetic package with the same name is not the historical release.
	// The migration acceptance run supplies an independently archived release.
	historical := os.Getenv("SAILRY_TEST_LEGACY_COMMANDS_DIR")
	if historical != "" {
		source = historical
	}
	access := sdktest.Inspect(w.Owner)
	commands, err := access.InstallPlugin(w.Ctx, source, "", "commands")
	if err != nil || len(commands.Issues) != 0 {
		w.T.Fatalf("commands fixture is invalid: %+v, %v", commands.Issues, err)
	}
	if err := access.Store().Node.Write(w.Ctx, func(db *sql.Conn) error {
		_, err := sdktest.InstallPlugin(w.Ctx, db, commands, 0, true)
		return err
	}); err != nil {
		w.T.Fatal(err)
	}
	if err := w.Controller.Refresh(w.Ctx); err != nil {
		w.T.Fatal(err)
	}

	for index, enabled := range []bool{true, false, true} {
		if index > 0 {
			if err := w.SetPluginEnabled(w.Ctx, "commands", enabled); err != nil {
				w.T.Fatal(err)
			}
		}
		w.Await("the commands package did not reach its selected state", func() bool {
			for _, summary := range w.Controller.Snapshot().Plugins {
				if summary.Name == "commands" {
					return summary.Enabled == enabled && summary.Compatibility.Kind == "compatible"
				}
			}
			return false
		})
		var items []string
		w.Await("the legacy package's unrelated menu contribution did not follow its enabled state", func() bool {
			items = w.ProjectMenu(project)
			w.Tester.CloseMenu()
			if historical != "" {
				return slices.Equal(items, nativeMenu)
			}
			return slices.Contains(items, probe) == enabled
		})
		want := nativeMenu
		if enabled && historical == "" {
			want = slices.Insert(slices.Clone(nativeMenu), terminal+1, "Legacy menu terminal", probe)
		}
		if !slices.Equal(items, want) {
			w.T.Fatalf("project menu with commands enabled=%v is %q, want %q", enabled, items, want)
		}
		checkTerminalAddMenu(w)
		stored := support.Result[protocol.PluginInfo](w.T, w.Do(protocol.Command{Kind: "read_plugin", Data: protocol.CommandReadPluginPayload{Name: "commands"}}))
		if sdktest.PackageReference(stored) != sdktest.PackageReference(commands) || stored.Summary.Enabled != enabled {
			w.T.Fatalf("native terminal UI changed the installed commands package: %+v", stored.Summary)
		}
	}
}

func checkTerminalAddMenu(w *support.Window) {
	w.T.Helper()
	w.OpenConversationDetails()
	if len(w.App.Panel.Tabs) == 0 {
		w.T.Fatal("the conversation needs an existing tab to show the details add menu")
	}
	var add ui.AccessInfo
	w.Await("the conversation details add button did not settle", func() bool {
		entry, ok := w.Tester.AccessByRole(ui.RoleMenuButton, support.Label("details"))
		b := entry.Bounds
		if !ok || entry.Disabled || !w.InDetails(entry) || b.Y+b.H > native.HeaderHeight {
			return false
		}
		previous := add.Bounds
		add = entry
		return b == previous
	})
	w.Click(add)
	var items []ui.AccessInfo
	w.Await("the conversation details add menu did not open", func() bool {
		items = items[:0]
		for _, entry := range w.Tester.AccessAll() {
			if entry.Name == support.Label("terminal") && !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0 {
				switch entry.Role {
				case ui.RoleMenuItem, ui.RoleMenuItemCheckBox, ui.RoleMenuItemRadio:
					items = append(items, entry)
				}
			}
		}
		return len(items) != 0
	})
	if len(items) != 1 || items[0].Disabled {
		w.T.Fatalf("the details add menu must offer one enabled Terminal item: %+v", items)
	}
	w.Tester.Key(0, ui.KeyEscape)
}
