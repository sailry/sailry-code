package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func TestFilesDirectoryWatchReleasesWithLastVisibleSurface(t *testing.T) {
	w := openFiles(t)
	root := t.TempDir()
	const entry = "watched.txt"
	if err := os.WriteFile(filepath.Join(root, entry), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	session := w.OpenProjectSession(w.CreateProject("Files watch lifetime", root).ID)
	observing := func() bool {
		_, watching := w.Controller.Workspace(session.Worktree, false)
		return watching
	}
	if observing() {
		t.Fatal("the conversation without a Files surface observed its directory")
	}
	for range 3 {
		w.App.Navigate(native.FilesPage)
		w.App.RevealDetails()
		w.access(ui.RoleTreeItem, entry)
		if !observing() {
			t.Fatal("the mounted Files page did not observe its directory")
		}
		w.App.Navigate(native.ConversationPage)
		w.Await("leaving the Files page did not release its directory observation", func() bool { return !observing() })

		w.App.RevealConversationPanel()
		w.App.Panel.Show(native.PanelTab{Kind: native.Files})
		w.access(ui.RoleTreeItem, entry)
		if !observing() {
			t.Fatal("the mounted conversation Files panel did not observe its directory")
		}
		w.App.PanelOpen = false
		w.Await("closing the Files panel did not release its directory observation", func() bool { return !observing() })
	}
}
