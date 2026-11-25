package codedesktop_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

func TestFilesAcrossDesktopAndNodeProducts(t *testing.T) {
	for _, host := range []struct {
		name     string
		assembly node.Assembly
	}{{"Harness Node", node.HarnessAssembly()}, {"Code Node", codehost.Assembly()}} {
		t.Run(host.name, func(t *testing.T) {
			for _, desktop := range []struct {
				name     string
				assembly native.Assembly
				manage   bool
			}{{"Harness Desktop", fixtures.HarnessAssembly(), false}, {"Code Desktop", codedesktop.Assembly(), true}} {
				t.Run(desktop.name, func(t *testing.T) {
					w := support.Open(t, host.assembly, desktop.assembly)
					w.App.SetWelcome(true)
					if installed := w.Controller.Snapshot().Plugins; len(installed) != 0 {
						t.Fatalf("fresh profile installed plugins: %+v", installed)
					}
					var listings atomic.Int32
					w.Controller.OnDispatch(func(kind string) {
						if kind == "list_directory" {
							listings.Add(1)
						}
					})
					const name, original = "source.txt", "explicit workspace file\n"
					folder := t.TempDir()
					path := filepath.Join(folder, name)
					if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
						t.Fatal(err)
					}
					project := w.CreateProject("File product fixture", folder)
					w.OpenProjectSession(project.ID)
					app := w.App
					if app.NativePageEnabled(native.FilesPage) != desktop.manage || desktop.assembly.HasPanelResource(native.Files) != desktop.manage {
						t.Fatalf("unexpected standalone or panel registration for %s", desktop.name)
					}
					if desktop.manage {
						app.Navigate(native.FilesPage)
						w.Await("Code Files did not list the file", func() bool {
							_, shown := w.Tester.AccessByRole(ui.RoleTreeItem, name)
							return app.Page == native.FilesPage && shown && listings.Load() != 0
						})
						app.Navigate(native.ConversationPage)
					} else {
						id := string(native.FilesPage)
						app.SetFeaturePreferences([]string{id}, map[string]bool{id: true})
						app.Do(id)
						w.Tester.Key(ui.Cmd, ui.KeyP)
						w.Tester.Frame()
						if order, pins := app.FeaturePreferences(); !slices.Contains(order, id) || !pins[id] {
							t.Fatal("omitting the page destroyed the saved rail choice")
						}
						if _, shown := w.Tester.AccessByRole(ui.RoleButton, support.Label("files")); shown || app.Page != native.ConversationPage || len(app.Panel.Tabs) != 0 {
							t.Fatal("saved rail choice or shortcut revived the unassembled file browser")
						}
					}

					var editor ui.AccessInfo
					var workspace *native.Project
					w.Await("the conversation did not bind the project editor", func() bool {
						workspace = app.Hooks.WorkspaceProject()
						return workspace != nil && workspace.Actions != nil && workspace.Actions.OpenFileAt != nil
					})
					workspace.Actions.OpenFileAt(name, 1)
					tester := w.Tester
					w.Await("the explicit path did not open in the conversation editor", func() bool {
						var shown bool
						editor, shown = tester.Access("Code editor")
						return shown && strings.TrimSuffix(editor.Value, "\n") == strings.TrimSuffix(original, "\n") && app.Panel.Active == (native.PanelTab{Kind: native.Files, ID: name})
					})
					tester.ClickAt(editor.Bounds.X+60, editor.Bounds.Y+8)
					w.Await("the file editor did not take keyboard focus", func() bool { return tester.Focused("Code editor") })
					tester.Type("X")
					var draft string
					w.Await("the explicit editor did not record a draft", func() bool {
						var dirty bool
						draft, dirty = workspace.FileDraft(name)
						return dirty && strings.Replace(draft, "X", "", 1) == original
					})
					tester.Key(ui.Cmd, ui.KeyS)
					w.Await("the edited file did not save through the selected Node", func() bool {
						saved, err := os.ReadFile(path)
						return err == nil && string(saved) == draft
					})
					if !desktop.manage {
						if _, shown := tester.AccessByRole(ui.RoleTreeItem, name); shown || listings.Load() != 0 {
							t.Fatal("explicit file use mounted an unassembled management tree")
						}
					}
				})
			}
		})
	}
}
