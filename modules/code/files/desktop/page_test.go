package desktop_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
)

func TestFilesPageUsesScopedWorkspaceAndRetainsDraft(t *testing.T) {
	const path = "scoped-note.txt"
	target := fixtures.ResourceTarget{Node: "remote", Project: "project", Worktree: "linked-worktree"}
	data := &fixtures.Data{
		Projects:  []*fixtures.Project{{ID: target.Project, Host: target.Node, Name: "Scoped project"}},
		Workspace: &fixtures.Workspace{},
	}
	workspace := &fixtures.Workspace{Files: &fixtures.FileNode{Name: "Worktree", Children: []*fixtures.FileNode{{Name: path, Path: path}}}}
	project := fixtures.NewProject(target.Worktree, workspace, nil)
	var read string
	project.Actions = &fixtures.Actions{
		OpenManagedFile: func(path string, _ int) { project.FileOpen(path) },
		ReadFile: func(path string) {
			read = path
			if !project.FileLoaded(path) {
				project.SetFile(path, "Saved text\n")
				project.SetFileWritable(path, true)
			}
		},
	}
	assembly := native.Assemble(files.Register)
	if pages := assembly.WorkspacePages; len(pages) != 1 || pages[0].Page != native.FilesPage || pages[0].Resource != native.Files || pages[0].RenderDetails == nil {
		t.Fatalf("the Files module did not declare its own workspace resource: %+v", pages)
	}
	app := fixtures.NewLive(data, assembly)
	app.Hooks.ResourceWorkspace = func(got fixtures.ResourceTarget) *native.Project {
		if got == target {
			return project
		}
		return nil
	}
	app.Navigate(native.FilesPage)
	app.SetResourceTarget(target)
	tester := ui.NewTesterWith(app.View, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tester.SetLocale("en")
	tester.Frame()
	tester.Advance(time.Second)
	tester.Frame()
	row, ok := tester.AccessByRole(ui.RoleTreeItem, path)
	if !ok {
		t.Fatalf("the selected worktree's file is missing: %q", tester.Texts())
	}
	tester.ClickAt(row.Bounds.X+row.Bounds.W/2, row.Bounds.Y+row.Bounds.H/2)
	tester.Frame()
	tester.Frame()
	if read != path || project.Files.File() != path || !project.FileLoaded(path) {
		t.Fatalf("opening a file selected %q and read %q", project.Files.File(), read)
	}
	editor, ok := tester.Access("Code editor")
	if !ok {
		t.Fatalf("the file editor is missing: %q", tester.Texts())
	}
	tester.ClickAt(editor.Bounds.X+120, editor.Bounds.Y+8)
	tester.Frame()
	tester.Frame()
	tester.Type("X")
	tester.Frame()
	tester.Frame()
	before, dirty := project.FileDraft(path)
	if !dirty || before == "Saved text\n" {
		t.Fatalf("typing left the draft %q, dirty = %v", before, dirty)
	}
	app.Navigate(native.ConversationPage)
	tester.Frame()
	app.Navigate(native.FilesPage)
	tester.Frame()
	if draft, dirty := project.FileDraft(path); !dirty || draft != before || !slices.Equal(project.Unsaved(), []string{path}) {
		t.Fatalf("draft after reopening Files = %q, dirty = %v, unsaved = %v", draft, dirty, project.Unsaved())
	}
}
