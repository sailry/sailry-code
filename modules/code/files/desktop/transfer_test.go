package desktop_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
)

func TestFilesWorkspaceTransferController(t *testing.T) {
	w := openFiles(t)
	folder := t.TempDir()
	external := t.TempDir()
	upload := filepath.Join(external, "upload.txt")
	download := filepath.Join(external, "download.txt")
	const moved = "the file moved through the Node\n"
	const sent = "the file picked on the controller\n"
	for name, contents := range map[string]string{filepath.Join(folder, "source.txt"): moved, upload: sent} {
		if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(folder, "destination"), 0o700); err != nil {
		t.Fatal(err)
	}
	var moves atomic.Int32
	w.Controller.OnDispatch(func(kind string) {
		if kind == "move_entry_to" {
			moves.Add(1)
		}
	})
	w.SetNativeFileActions(fixtures.NativeFileActions{
		LocalNode: w.Owner.ID(), PickFiles: func(picked func([]string)) { picked([]string{upload}) },
		SaveFile: func(_ string, picked func(string)) { picked(download) },
	})
	w.SetWorkspaceNative(fixtures.WorkspaceFileActions{Download: w.DownloadWorkspaceFile})
	project := w.CreateProject("Native transfers", folder)
	w.OpenProjectSession(project.ID)
	w.App.Navigate(native.FilesPage)
	menu := func(name, key string) {
		t.Helper()
		b := w.access(ui.RoleTreeItem, name).Bounds
		w.Tester.RightClickAt(b.X+b.W/2, b.Y+b.H/2)
		if err := w.Tester.ChooseMenuItem(support.Label(key)); err != nil {
			t.Fatal(err)
		}
	}
	menu("source.txt", "files_cut")
	menu("destination", "files_paste")
	w.control(ui.RoleTextField, "files_paste_directory")
	w.Click(w.control(ui.RoleButton, "settings_cancel"))
	w.Await("cancelled transfer form is still open", func() bool {
		_, found := w.Tester.AccessByRole(ui.RoleTextField, support.Label("files_paste_directory"))
		return !found
	})
	if moves.Load() != 0 {
		t.Fatal("an unconfirmed move was sent to the Node")
	}
	if data, err := os.ReadFile(filepath.Join(folder, "source.txt")); err != nil || string(data) != moved {
		t.Fatalf("cancellation changed the source: %q, %v", data, err)
	}
	menu("destination", "files_paste")
	w.Click(w.control(ui.RoleButton, "files_move"))
	w.Await("move did not publish the destination", func() bool {
		data, err := os.ReadFile(filepath.Join(folder, "destination", "source.txt"))
		return err == nil && string(data) == moved
	})
	if _, err := os.Stat(filepath.Join(folder, "source.txt")); !os.IsNotExist(err) || moves.Load() != 1 {
		t.Fatalf("move left the source or sent duplicate requests: %v, %d", err, moves.Load())
	}
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("move dialog did not close", func() bool {
		_, found := w.Tester.AccessByRole(ui.RoleTextField, support.Label("files_paste_directory"))
		return !found
	})
	w.Click(w.access(ui.RoleTreeItem, "destination"))
	if err := w.Tester.Click(support.Label("files_create")); err != nil {
		t.Fatal(err)
	}
	w.Click(w.control(ui.RoleMenuItem, "files_upload"))
	w.Await("upload did not publish the picked bytes", func() bool {
		data, err := os.ReadFile(filepath.Join(folder, "destination", "upload.txt"))
		return err == nil && string(data) == sent
	})
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("upload dialog did not close", func() bool { return !w.Tester.HasText("destination/upload.txt") })
	menu("upload.txt", "files_download")
	w.Await("download did not publish to the controller's chosen path", func() bool {
		data, err := os.ReadFile(download)
		return err == nil && string(data) == sent
	})
}
