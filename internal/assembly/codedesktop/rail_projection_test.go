package codedesktop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func TestRailFilesAndGitProjection(t *testing.T) {
	folder := t.TempDir()
	const path = "notes.txt"
	const change = "shared workspace change"
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(folder, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", folder, "-c", "user.name=Rail", "-c", "user.email=rail@example.invalid", "-c", "commit.gpgsign=false"}, arguments...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	write("first line\n")
	git("init", "-q")
	git("add", path)
	git("commit", "-q", "-m", "Add the notes")
	write("first line\n" + change + "\n")

	w := support.Open(t, codehost.Assembly(), codedesktop.Assembly())
	project := w.CreateProject("Rail workspace", folder)
	session := w.OpenProjectSession(project.ID)
	app := w.App
	var workspace *native.Project
	w.Await("the session did not bind its workspace", func() bool {
		workspace = app.Hooks.WorkspaceProject()
		return workspace != nil && workspace.ID == session.Worktree.String()
	})
	if app.PanelOpen || len(app.Panel.Tabs) != 0 {
		t.Fatalf("the panel before the pages: open %v, tabs %v", app.PanelOpen, app.Panel.Tabs)
	}
	rail := func(key string) {
		t.Helper()
		var entry ui.AccessInfo
		w.Await("no "+key+" on the product rail", func() bool {
			for _, item := range w.Tester.AccessAll() {
				b := item.Bounds
				if item.Role == ui.RoleButton && item.Name == support.Label(key) && !item.Offscreen && b.W > 0 && b.X+b.W <= native.RailWidth {
					entry = item
					return true
				}
			}
			return false
		})
		w.Click(entry)
	}
	for _, surface := range []struct {
		feature string
		page    native.Page
		open    func() string
	}{{"files", native.FilesPage, workspace.Files.File}, {"git", native.GitPage, workspace.Git.Change}} {
		rail(surface.feature)
		var row ui.AccessInfo
		w.Await("the "+surface.feature+" page did not show the worktree entry", func() bool {
			var listed bool
			row, listed = w.Tester.AccessByRole(ui.RoleTreeItem, path)
			return app.Page == surface.page && listed && !row.Offscreen && row.Bounds.W > 0
		})
		w.Click(row)
		w.Await("the "+surface.feature+" page did not open the worktree entry", func() bool {
			for _, entry := range w.Tester.AccessAll() {
				if strings.Contains(entry.Value, change) {
					return surface.open() == path
				}
			}
			return false
		})
		if app.PanelOpen {
			t.Fatalf("the %s page opened the conversation panel", surface.feature)
		}
	}
	rail("shell_sessions")
	w.OpenConversationDetails()
	w.Await("the conversation panel did not share the file and diff opened on the rail pages", func() bool {
		var file, diff bool
		for _, tab := range app.Panel.Tabs {
			file = file || tab.Kind == native.Files && tab.ID == path
			diff = diff || tab.Kind == native.Changes && tab.ID == path
		}
		return file && diff
	})
}
