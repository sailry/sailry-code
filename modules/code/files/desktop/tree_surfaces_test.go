package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func TestFilesTreeSharesSelectionAndExpansionAcrossSurfaces(t *testing.T) {
	w := openFiles(t)
	folder := t.TempDir()
	if err := os.Mkdir(filepath.Join(folder, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	const name = "note.txt"
	if err := os.WriteFile(filepath.Join(folder, "docs", name), []byte("Tree across both surfaces\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := w.CreateProject("Shared Files tree", folder)
	session := w.OpenProjectSession(project.ID)
	workspace := w.App.Hooks.WorkspaceProject()
	w.App.Navigate(native.FilesPage)
	w.App.RevealDetails()
	w.Click(w.access(ui.RoleTreeItem, "docs"))
	b := w.access(ui.RoleTreeItem, name).Bounds
	w.Tester.ClickAtWith(ui.Cmd, b.X+b.W/2, b.Y+b.H/2)
	w.Await("the page did not select its file", func() bool { entry, ok := w.Tester.AccessByRole(ui.RoleTreeItem, name); return ok && entry.Selected })
	if workspace.Files.File() != "" {
		t.Fatal("modifier click opened a document")
	}
	w.App.Navigate(native.ConversationPage)
	w.OpenConversationDetails()
	w.Tester.Key(ui.Cmd, ui.KeyP)
	w.Await("the sidebar did not preserve the page's expanded folder and selected row", func() bool {
		entry, ok := w.Tester.AccessByRole(ui.RoleTreeItem, name)
		return ok && entry.Selected && !entry.Offscreen
	})
	w.Click(w.access(ui.RoleTreeItem, name))
	w.Await("opening the sidebar row did not show its document", func() bool {
		return w.App.Panel.Active == (native.PanelTab{Kind: native.Files, ID: "docs/" + name}) && w.editorContains("Tree across both surfaces")
	})
	w.App.RevealConversationPanel()
	if _, found := w.Controller.Workspace(session.Worktree, false); !found {
		t.Fatal("the rendered browser has no observation owner")
	}
	const attempts = 3
	for range attempts {
		w.App.Navigate(native.FilesPage)
		w.App.RevealDetails()
		w.Await("standalone Files did not render the open sidebar document", func() bool { return w.editorContains("Tree across both surfaces") })
		w.App.Navigate(native.ConversationPage)
		w.OpenConversationDetails()
		w.access(ui.RoleTreeItem, name)
	}
}
