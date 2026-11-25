package codedesktop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func TestWorkspaceResourcesAssembleIndependently(t *testing.T) {
	folder := t.TempDir()
	init := exec.Command("git", "-C", folder, "init", "-q", "-b", "main")
	init.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	const name = "workspace-resource.txt"
	if err := os.WriteFile(filepath.Join(folder, name), []byte("workspace resource fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		modules []native.Module
		files   bool
		git     bool
	}{
		{name: "neither"},
		{name: "files_only", modules: []native.Module{files.Register}, files: true},
		{name: "git_only", modules: []native.Module{git.Register}, git: true},
		{name: "both", modules: []native.Module{files.Register, git.Register}, files: true, git: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var fileReads, gitReads atomic.Int32
			t.Cleanup(func() {
				if count := fileReads.Load(); (count != 0) != test.files {
					t.Errorf("list_directory dispatched %d times, Files assembled = %t", count, test.files)
				}
				if count := gitReads.Load(); (count != 0) != test.git {
					t.Errorf("inspect_git dispatched %d times, Git assembled = %t", count, test.git)
				}
			})
			w := support.Open(t, codehost.Assembly(), native.Assemble(test.modules...))
			w.Controller.OnDispatch(func(kind string) {
				switch kind {
				case "list_directory":
					fileReads.Add(1)
				case "inspect_git":
					gitReads.Add(1)
				}
			})
			project := w.CreateProject("Workspace resource fixture", folder)
			session := w.OpenProjectSession(project.ID)
			launcher := w.DetailsLauncherButtons()
			for _, entry := range launcher {
				if entry.Name == support.Label("resource_browser") {
					t.Fatalf("the launcher offered Browser without its module: %+v", entry)
				}
			}

			resources := []struct {
				label   string
				page    native.Page
				mods    ui.Modifiers
				key     ui.Key
				enabled bool
				rail    ui.AccessInfo
			}{
				{label: "files", page: native.FilesPage, mods: ui.Cmd, key: ui.KeyP, enabled: test.files},
				{label: "git", page: native.GitPage, mods: ui.Ctrl | ui.Shift, key: ui.KeyG, enabled: test.git},
			}
			for i := range resources {
				resource := &resources[i]
				label := support.Label(resource.label)
				var railCount, launcherCount int
				for _, entry := range launcher {
					if entry.Name == label {
						launcherCount++
						if resource.enabled && entry.Disabled {
							t.Errorf("the assembled %s launcher entry is disabled", resource.label)
						}
					}
				}
				for _, entry := range w.Tester.AccessAll() {
					b := entry.Bounds
					if entry.Role == ui.RoleButton && entry.Name == label && !entry.Offscreen && b.W > 0 && b.H > 0 && b.X+b.W <= native.RailWidth {
						railCount++
						resource.rail = entry
					}
				}
				want := 0
				if resource.enabled {
					want = 1
				}
				if w.App.NativePageEnabled(resource.page) != resource.enabled || railCount != want || launcherCount != want {
					t.Errorf("%s assembled=%t: page registered=%t, rail entries=%d, launcher entries=%d", resource.label,
						resource.enabled, w.App.NativePageEnabled(resource.page), railCount, launcherCount)
				}
				if !resource.enabled {
					w.App.Navigate(resource.page)
					w.Tester.Key(resource.mods, resource.key)
					w.Tester.Frame()
					if w.App.Page != native.ConversationPage || w.App.FocusedConversation() != session.ID.String() || len(w.App.Panel.Tabs) != 0 {
						t.Errorf("unassembled %s opened page %q or tab %v", resource.label, w.App.Page, w.App.Panel.Tabs)
					}
					continue
				}
				if railCount != 1 || resource.rail.Disabled {
					t.Fatalf("expected one enabled %s rail entry, got %d: %+v", resource.label, railCount, resource.rail)
				}
			}
			for _, resource := range resources {
				if !resource.enabled {
					continue
				}
				w.Click(resource.rail)
				w.Await("the "+resource.label+" page did not load the worktree entry", func() bool {
					entry, ok := w.Tester.AccessByRole(ui.RoleTreeItem, name)
					return w.App.Page == resource.page && ok && !entry.Offscreen && entry.Bounds.W > 0
				})
			}
		})
	}
}
