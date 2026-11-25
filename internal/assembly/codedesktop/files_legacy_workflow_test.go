package codedesktop_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestLegacyFilesInstalledDesktopWorkflow(t *testing.T) {
	n, before := installOfficialFiles(t, node.HarnessAssembly())
	identity := n.Owner.ID()
	if err := n.Owner.Stop(n.Ctx); err != nil {
		t.Fatal(err)
	}
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "SHELL=/bin/sh"})
	owner, err := node.StartWithOptions(n.Ctx, n.Profile, node.Options{Assembly: node.HarnessAssembly(), Environment: &environment})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	n.Owner = owner
	if owner.ID() != identity {
		t.Fatal("opening the existing profile replaced the Node identity")
	}

	const name, target = "legacy-source.txt", "legacy-target"
	original := []byte("workspace text before editing\n")
	folder := t.TempDir()
	if err := os.Mkdir(filepath.Join(folder, target), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, name)
	if err := os.WriteFile(file, original, 0o600); err != nil {
		t.Fatal(err)
	}
	w := n.Connect(fixtures.HarnessAssembly())
	requests := observeFilesFixtureRequests(w)
	w.App.SetWelcome(true)
	if w.App.NativePageEnabled(native.FilesPage) || fixtures.HarnessAssembly().HasPanelResource(native.Files) {
		t.Fatal("the fixture must not include the native Files workbench")
	}
	w.Await("Harness did not show the installed Files rail entry", func() bool {
		_, ok := w.Tester.AccessByRole(ui.RoleButton, support.Label("files"))
		return ok
	})
	project := w.CreateProject("Legacy Files workflow", folder)
	session := w.OpenProjectSession(project.ID)
	route := "plugin:" + n.Owner.ID().String() + "/files"
	w.App.Do(route)
	legacyFilesCopy(w, name)
	openFilesFixtureDocument(w, native.Page("page:plugin"), route, name, original, requests)
	legacyFilesChooseDestination(w, target)
	expected := saveFilesFixtureDocument(w, file, original, "X", requests)

	legacyFilesPaste(w)
	awaitLegacyFilesTransfer(w, name, target)
	legacyFilesButton(w, "Cancel", "the legacy prepared transfer had no Cancel action")
	w.Await("Cancel did not dismiss the legacy transfer dialog", func() bool {
		_, shown := w.Tester.AccessByRole(ui.RoleButton, "Cancel")
		return !shown
	})
	destination := filepath.Join(folder, target, name)
	if _, err := os.Stat(destination); !os.IsNotExist(err) || requests.copies.Load() != 0 {
		t.Fatalf("a cancelled transfer submitted a write: %v, %d copy commands", err, requests.copies.Load())
	}
	legacyFilesPaste(w)
	awaitLegacyFilesTransfer(w, name, target)
	legacyFilesButton(w, "Paste", "the prepared legacy transfer had no Paste action")
	w.Await("the legacy paste did not copy the saved data through the Node", func() bool {
		copied, err := os.ReadFile(destination)
		return err == nil && bytes.Equal(copied, expected) && requests.copies.Load() == 1
	})

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "separate.txt"), []byte("different project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherProject := w.CreateProject("Separate Files workspace", other)
	w.OpenProjectSession(otherProject.ID)
	w.App.Do(route)
	w.Await("the Files view did not change to the second project", func() bool {
		_, entry := w.Tester.AccessByRole(ui.RoleTreeItem, "separate.txt")
		return entry
	})
	if _, shown := w.Tester.AccessByRole(ui.RoleTreeItem, name); shown || slices.Contains(w.Tester.Texts(), name) {
		t.Fatal("the other worktree displayed the previous worktree's file or document")
	}
	w.OpenSession(session.ID.String())
	w.Await("the original project session was not restored", func() bool {
		return w.App.Page == native.ConversationPage && w.App.FocusedConversation() == session.ID.String()
	})
	w.App.Do(route)
	w.Await("switching projects lost the original legacy document", func() bool {
		return filesFixtureEditorShows(w, expected)
	})
	assertFilesFixtureInstallation(w, before)
}

type filesFixtureRequests struct {
	reads, writes, copies atomic.Int32
}

func observeFilesFixtureRequests(w *support.Window) *filesFixtureRequests {
	requests := &filesFixtureRequests{}
	w.Controller.OnDispatch(func(kind string) {
		switch kind {
		case "read_file":
			requests.reads.Add(1)
		case "write_file":
			requests.writes.Add(1)
		case "copy_entry_to":
			requests.copies.Add(1)
		}
	})
	return requests
}

func openFilesFixtureDocument(w *support.Window, page native.Page, route, name string, expected []byte, requests *filesFixtureRequests) {
	w.T.Helper()
	current := func() bool { return w.App.Page == page && (route == "" || w.App.PluginFeature() == route) }
	var row ui.AccessInfo
	w.Await("the Files tree did not list a clickable row for "+name, func() bool {
		var shown bool
		row, shown = w.Tester.AccessByRole(ui.RoleTreeItem, name)
		return current() && shown && filesFixtureRowInWindow(row)
	})
	before := requests.reads.Load()
	defer func() {
		if w.T.Failed() {
			w.T.Logf("clicked file row at %+v; read_file requests from %q: %d after click", row.Bounds, page, requests.reads.Load()-before)
		}
	}()
	w.Click(row)
	w.Await("Files did not read and render the worktree file's contents", func() bool {
		return current() && requests.reads.Load() > before && filesFixtureEditorShows(w, expected)
	})
}

func filesFixtureRowInWindow(row ui.AccessInfo) bool {
	// The sliding details column exposes rows before their bounds fit in the window.
	b := row.Bounds
	return !row.Offscreen && !row.Disabled && b.W > 0 && b.H > 0 && b.X >= 0 && b.Y >= 0 &&
		b.X+b.W <= support.Width && b.Y+b.H <= support.Height
}

func filesFixtureEditorShows(w *support.Window, expected []byte) bool {
	editor, shown := w.Tester.Access("Code editor")
	return shown && strings.TrimSuffix(editor.Value, "\n") == strings.TrimSuffix(string(expected), "\n")
}

func saveFilesFixtureDocument(w *support.Window, file string, original []byte, marker string, requests *filesFixtureRequests) []byte {
	w.T.Helper()
	editor, shown := w.Tester.Access("Code editor")
	if !shown || editor.ReadOnly || editor.Disabled {
		w.T.Fatal("Files did not present a writable editor")
	}
	w.Tester.ClickAt(editor.Bounds.X+min(60, editor.Bounds.W/2), editor.Bounds.Y+min(8, editor.Bounds.H/2))
	w.Await("the Files editor did not take focus", func() bool { return w.Tester.Focused("Code editor") })
	w.Tester.Type(marker)
	w.Await("the Files editor did not retain the edit", func() bool {
		editor, visible := w.Tester.Access("Code editor")
		return visible && strings.Contains(editor.Value, marker)
	})
	before := requests.writes.Load()
	legacyFilesButton(w, "Save", "the Files page Save action was not enabled")
	var saved []byte
	w.Await("the Files edit did not save to the original worktree", func() bool {
		var err error
		saved, err = os.ReadFile(file)
		inserted := []byte(marker)
		return err == nil && requests.writes.Load() > before && bytes.Contains(saved, inserted) && bytes.Equal(bytes.Replace(saved, inserted, nil, 1), original)
	})
	return saved
}

func assertFilesFixtureInstallation(w *support.Window, before protocol.PluginSummary) {
	w.T.Helper()
	installed := w.Controller.Snapshot().Plugins
	if len(installed) != 1 || installed[0].Version == nil || before.Version == nil || *installed[0].Version != *before.Version ||
		installed[0].Digest != before.Digest || installed[0].Revision != before.Revision || installed[0].SettingsRevision != before.SettingsRevision || !installed[0].Enabled {
		w.T.Fatalf("using Files changed the installed package: %+v", installed)
	}
}

func awaitLegacyFilesTransfer(w *support.Window, source, target string) {
	w.T.Helper()
	w.Await("the selected legacy Paste action did not prepare "+target+"/"+source, func() bool {
		field, shown := w.Tester.AccessByRole(ui.RoleTextField, "Name")
		texts := w.Tester.Texts()
		directory := slices.Index(texts, "Destination folder")
		return shown && !field.Offscreen && field.Value == source && directory >= 0 && directory+1 < len(texts) && texts[directory+1] == target
	})
}

func legacyFilesTreeRow(w *support.Window, name string) ui.AccessInfo {
	w.T.Helper()
	var row ui.AccessInfo
	w.Await("missing legacy tree row "+name, func() bool {
		var found bool
		row, found = w.Tester.AccessByRole(ui.RoleTreeItem, name)
		return found && filesFixtureRowInWindow(row)
	})
	return row
}

func legacyFilesCopy(w *support.Window, name string) {
	w.T.Helper()
	b := legacyFilesTreeRow(w, name).Bounds
	// Select without opening a document: opening would redirect the copy shortcut to its editor.
	w.Tester.ClickAtWith(ui.Shift, b.X+b.W/2, b.Y+b.H/2)
	w.Await("the source row did not receive keyboard focus", func() bool {
		row, found := w.Tester.AccessByRole(ui.RoleTreeItem, name)
		return found && row.Selected && row.Focused
	})
	w.Tester.Key(ui.Cmd, ui.KeyC)
	legacyFilesPasteMenu(w)
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("the Files toolbar menu did not close", func() bool {
		_, shown := w.Tester.AccessByRole(ui.RoleMenuItem, "Paste")
		return !shown
	})
}

func legacyFilesChooseDestination(w *support.Window, name string) {
	w.T.Helper()
	b := legacyFilesTreeRow(w, name).Bounds
	w.Tester.RightClickAt(b.X+min(28, b.W/2), b.Y+b.H/2)
	if len(w.Tester.Menu()) == 0 {
		w.T.Fatal("the destination did not open its native context menu")
	}
	w.Tester.CloseMenu()
	w.Await("the destination folder did not become selected", func() bool {
		row, found := w.Tester.AccessByRole(ui.RoleTreeItem, name)
		return found && row.Selected
	})
}

func legacyFilesPaste(w *support.Window) {
	w.T.Helper()
	w.Click(legacyFilesPasteMenu(w))
}

func legacyFilesPasteMenu(w *support.Window) ui.AccessInfo {
	w.T.Helper()
	var create, paste ui.AccessInfo
	w.Await("the legacy Files toolbar had no Create action", func() bool {
		var found bool
		create, found = w.Tester.AccessByRole(ui.RoleMenuButton, "Create")
		return found && filesFixtureRowInWindow(create)
	})
	w.Click(create)
	w.Await("the legacy Files menu had no enabled Paste action", func() bool {
		var found bool
		paste, found = w.Tester.AccessByRole(ui.RoleMenuItem, "Paste")
		return found && filesFixtureRowInWindow(paste)
	})
	return paste
}

func legacyFilesButton(w *support.Window, name, failure string) {
	w.T.Helper()
	var button ui.AccessInfo
	w.Await(failure, func() bool {
		var found bool
		button, found = w.Tester.AccessByRole(ui.RoleButton, name)
		return found && !button.Offscreen && !button.Disabled
	})
	w.Click(button)
}
