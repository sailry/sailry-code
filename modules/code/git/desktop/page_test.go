package desktop_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	shell "github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
)

func TestGitOpenSourceUsesRegisteredFilesPage(t *testing.T) {
	for _, test := range []struct {
		name      string
		filesPage bool
	}{
		{name: "Git only"},
		{name: "Git and Files", filesPage: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			const path = "changed.txt"
			target := fixtures.ResourceTarget{Node: "remote", Project: "project", Worktree: "linked-worktree"}
			data := &fixtures.Data{
				Projects:  []*fixtures.Project{{ID: target.Project, Host: target.Node, Name: "Scoped project"}},
				Workspace: &fixtures.Workspace{},
			}
			workspace := &fixtures.Workspace{Changes: []fixtures.FileChange{{Path: path, Status: "M", Unstaged: "M"}}}
			if test.filesPage {
				workspace.Files = &fixtures.FileNode{Name: "Worktree", Children: []*fixtures.FileNode{{Name: path, Path: path}}}
			}
			var read string
			project := fixtures.NewProject(target.Worktree, workspace, &fixtures.Actions{ReadFile: func(path string) { read = path }})
			project.SetGitState(fixtures.GitState{Known: true, Head: true})
			modules := []shell.Module{git.Register}
			if test.filesPage {
				modules = append(modules, files.Register)
			}
			assembly := shell.Assemble(modules...)
			if len(assembly.SettingsPages) != 0 || assembly.HasPanelResource(shell.Terminal) || !assembly.HasPanelResource(shell.Changes) {
				t.Fatal("Git must contribute its panel without unrelated features")
			}
			if pages := assembly.WorkspacePages; len(pages) != len(modules) || pages[0].Page != shell.GitPage || pages[0].Resource != shell.Changes || pages[0].RenderDetails == nil {
				t.Fatalf("the Git module did not declare its own workspace resource: %+v", pages)
			}
			app := fixtures.NewLive(data, assembly)
			app.Hooks.ResourceWorkspace = func(got fixtures.ResourceTarget) *shell.Project {
				if got == target {
					return project
				}
				return nil
			}
			app.Navigate(shell.GitPage)
			app.SetResourceTarget(target)
			tester := ui.NewTesterWith(app.View, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
			tester.SetLocale("en")
			tester.Frame()
			tester.Advance(time.Second)
			tester.Frame()
			row, ok := tester.AccessByRole(ui.RoleTreeItem, path)
			if !ok {
				t.Fatalf("the selected worktree's change is missing: %q", tester.Texts())
			}
			tester.RightClickAt(row.Bounds.X+row.Bounds.W/2, row.Bounds.Y+row.Bounds.H/2)
			items := tester.Menu()
			openSource := i18n.In("en", "git_open_source")
			if !slices.Contains(items, i18n.In("en", "git_unstaged")) || slices.Contains(items, openSource) != test.filesPage {
				t.Fatalf("change menu with Files installed = %v: %q", test.filesPage, items)
			}
			if !test.filesPage {
				if app.NativePageEnabled(shell.FilesPage) || app.RevealResourceFile() != nil || read != "" {
					t.Fatalf("Git alone enabled a Files route or read %q", read)
				}
				return
			}
			if err := tester.ChooseMenuItem(openSource); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			if app.Page != shell.FilesPage || app.ResourceTarget() != target || project.Files.File() != path || read != path {
				t.Fatalf("Open source navigated to %q on %+v, selected %q and read %q", app.Page, app.ResourceTarget(), project.Files.File(), read)
			}
		})
	}
}

func TestGitPreviewOpenSource(t *testing.T) {
	for _, test := range []struct {
		name      string
		assembly  shell.Assembly
		canReveal bool
	}{
		{"standalone", shell.Assemble(git.Register), false},
		{"with Files", shell.Assemble(git.Register, files.Register), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := shell.NewPreview(test.assembly)
			app.Navigate(shell.GitPage)
			tester := ui.NewTesterWith(app.View, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
			tester.SetLocale("en")
			tester.Frame()
			tester.Advance(time.Second)
			tester.Frame()
			row, ok := tester.AccessByRole(ui.RoleTreeItem, "example.md")
			if app.Page != shell.GitPage || !ok {
				t.Fatalf("the Git page did not render the preview change: %q", tester.Texts())
			}
			tester.RightClickAt(row.Bounds.X+row.Bounds.W/2, row.Bounds.Y+row.Bounds.H/2)
			open := i18n.In("en", "git_open_source")
			if menu := tester.Menu(); slices.Contains(menu, open) != test.canReveal || !slices.Contains(menu, i18n.In("en", "git_discard_changes")) {
				t.Fatalf("unexpected change menu: %v", menu)
			}
			if !test.canReveal {
				return
			}
			if err := tester.ChooseMenuItem(open); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			project := app.ResourceProject()
			if app.Page != shell.FilesPage || project == nil || app.Projects.Of(project.ID).Files.File() != "sailry-go/docs/example.md" {
				t.Fatalf("the source file did not open in Files: %q", tester.Texts())
			}
		})
	}
}

func TestGitPageWithoutProject(t *testing.T) {
	app := fixtures.NewLive(&fixtures.Data{Workspace: &fixtures.Workspace{}}, shell.Assemble(git.Register))
	app.Navigate(shell.GitPage)
	tester := ui.NewTesterWith(app.View, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true, ReduceMotion: true}})
	tester.SetLocale("en")
	tester.Frame()
	text := func(key string) string { return i18n.In("en", key) }
	if app.Page != shell.GitPage || !tester.HasText(text("resource_project_empty")) || tester.HasText(text("unavailable")) {
		t.Fatalf("the Git page without a project did not offer project selection: %q", tester.Texts())
	}
	if err := tester.Click(text("project_new")); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !app.Sidebar.Editing() || !tester.HasText(text("project_name")) {
		t.Fatalf("New project did not open the project editor: %q", tester.Texts())
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if app.Sidebar.Editing() {
		t.Fatal("Escape left the project editor open")
	}
}
