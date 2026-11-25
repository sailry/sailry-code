package desktop_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

func TestFilesWorkspaceReadsAndOpensSearchResult(t *testing.T) {
	w := openFiles(t)
	if installed := w.Controller.Snapshot().Plugins; len(installed) != 0 {
		t.Fatalf("native Files page installed packages: %+v", installed)
	}
	w.Await("the new profile did not open on a draft", func() bool { return strings.HasPrefix(w.App.Sidebar.Selected, "draft/") })
	w.Click(w.control(ui.RoleButton, "files"))
	w.Await("Files did not ask for a project", func() bool {
		return w.App.Page == native.FilesPage && w.Tester.HasText(support.Label("resource_project_empty"))
	})
	if w.Tester.HasText(support.Label("unavailable")) {
		t.Fatalf("the empty page is unavailable: %q", w.Tester.Texts())
	}
	w.Click(w.control(ui.RoleButton, "project_new"))
	w.Await("New project did not open the editor", func() bool {
		return w.App.Sidebar.Editing() && w.Tester.HasText(support.Label("project_name"))
	})
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("Escape did not close the project editor", func() bool { return !w.App.Sidebar.Editing() })

	folder := t.TempDir()
	for name, text := range map[string]string{"notes.txt": "first line\nsearch result line\n", "main.go": "package railpages\n"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	project := w.CreateProject("Rail workspace", folder)
	session := w.OpenProjectSession(project.ID)
	var workspace *native.Project
	w.Await("the session did not bind the worktree", func() bool {
		workspace = w.App.Hooks.WorkspaceProject()
		return workspace != nil && workspace.ID == session.Worktree.String()
	})
	if w.App.PanelOpen || len(w.App.Panel.Tabs) != 0 {
		t.Fatalf("the panel before Files: open %v, tabs %v", w.App.PanelOpen, w.App.Panel.Tabs)
	}
	w.Click(w.control(ui.RoleButton, "files"))
	w.Click(w.access(ui.RoleTreeItem, "main.go"))
	w.Await("the selected file did not open", func() bool {
		return w.App.Page == native.FilesPage && workspace.Files.File() == "main.go" && w.editorContains("package railpages")
	})

	w.Click(w.control(ui.RoleButton, "file_search"))
	w.Click(w.control(ui.RoleTextField, "file_search_query"))
	w.Tester.Type("search result line")
	w.Tester.Key(0, ui.KeyEnter)
	w.Click(w.access(ui.RoleButton, "notes.txt:2"))
	w.Await("the search result did not open in the page editor", func() bool {
		return workspace.Files.File() == "notes.txt" && w.editorContains("search result line")
	})
	if w.App.Page != native.FilesPage || w.App.PanelOpen {
		t.Fatal("a search result opened behind the Files page")
	}
	w.Click(w.control(ui.RoleButton, "shell_sessions"))
	w.OpenConversationDetails()
	w.Await("the conversation did not share the opened file tab", func() bool {
		return slices.Contains(w.App.Panel.Tabs, native.PanelTab{Kind: native.Files, ID: "notes.txt"})
	})
}

func TestFilesWorkspaceManagesEntries(t *testing.T) {
	folder := t.TempDir()
	const relative = "docs/copies/copy.txt"
	w, recovered := openFilesWithTrash(t, folder, relative, "")
	var trashed atomic.Int32
	w.Controller.OnDispatch(func(kind string) {
		if kind == "trash_entry" {
			trashed.Add(1)
		}
	})
	project := w.CreateProject("Entry actions", folder)
	session := w.OpenProjectSession(project.ID)
	w.Click(w.control(ui.RoleButton, "files"))
	entry := func(label, name string) {
		t.Helper()
		if err := w.Tester.Click(support.Label("files_create")); err != nil {
			t.Fatal(err)
		}
		w.Click(w.control(ui.RoleMenuItem, label))
		w.nameEntry(name)
	}
	entry("files_new_directory", "docs")
	w.Click(w.access(ui.RoleTreeItem, "docs"))
	entry("files_new_file", "draft.txt")
	w.Await("creating the selected folder's file did not open the editor", func() bool {
		workspace := w.App.Hooks.WorkspaceProject()
		return workspace != nil && workspace.Files.File() == "docs/draft.txt" && workspace.FileLoaded("docs/draft.txt")
	})
	row := w.access(ui.RoleTreeItem, "draft.txt")
	b := row.Bounds
	w.Tester.RightClickAt(b.X+b.W/2, b.Y+b.H/2)
	for _, key := range []string{"files_open_system", "files_copy", "files_cut", "files_copy_path", "files_insert_conversation", "files_download", "files_trash"} {
		if !slices.Contains(w.Tester.Menu(), support.Label(key)) {
			t.Errorf("file menu lacks %s: %q", key, w.Tester.Menu())
		}
	}
	if err := w.Tester.ChooseMenuItem(support.Label("files_rename")); err != nil {
		t.Fatal(err)
	}
	w.nameEntry("renamed.txt")
	w.access(ui.RoleTreeItem, "renamed.txt")
	if _, err := os.Stat(filepath.Join(folder, "docs", "draft.txt")); !os.IsNotExist(err) {
		t.Fatalf("original file remained after rename: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(folder, "docs", "renamed.txt")); err != nil || len(data) != 0 {
		t.Fatalf("renamed file = %q, %v", data, err)
	}
	menuAction := func(name, key string) {
		t.Helper()
		b := w.access(ui.RoleTreeItem, name).Bounds
		w.Tester.RightClickAt(b.X+b.W/2, b.Y+b.H/2)
		if err := w.Tester.ChooseMenuItem(support.Label(key)); err != nil {
			t.Fatal(err)
		}
	}
	menuAction("renamed.txt", "files_copy_path")
	if got := w.Tester.Clipboard(); got != "docs/renamed.txt" {
		t.Fatalf("copied selected path %q", got)
	}
	menuAction("renamed.txt", "files_copy")
	entry("files_new_directory", "copies")
	w.Click(w.access(ui.RoleTreeItem, "copies"))
	menuAction("copies", "files_paste")
	w.Click(w.control(ui.RoleTextField, "files_paste_directory"))
	w.Tester.Key(ui.Cmd, ui.KeyA)
	w.Tester.Type("docs/copies/copy.txt")
	copy := filepath.Join(folder, filepath.FromSlash(relative))
	if _, err := os.Stat(copy); !os.IsNotExist(err) {
		t.Fatalf("paste wrote before confirmation: %v", err)
	}
	w.Click(w.control(ui.RoleButton, "files_paste"))
	w.Await("paste did not write the destination", func() bool {
		_, err := os.Stat(copy)
		return err == nil
	})
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("the paste dialog did not close", func() bool {
		_, found := w.Tester.AccessByRole(ui.RoleTextField, support.Label("files_paste_directory"))
		return !found
	})
	menuAction("copy.txt", "files_trash")
	confirm := w.control(ui.RoleButton, "files_trash")
	original, err := os.Stat(copy)
	if err != nil || trashed.Load() != 0 || !w.Tester.HasText(relative) {
		t.Fatalf("trash was dispatched before confirmation or did not name %q: requests=%d, entry=%v", relative, trashed.Load(), err)
	}
	w.Click(confirm)
	w.Await("trash did not move the captured entry and report success", func() bool {
		_, err := os.Stat(copy)
		_, shown := w.Tester.AccessByRole(ui.RoleTreeItem, "copy.txt")
		return os.IsNotExist(err) && !shown && w.Tester.HasText(support.Label("workspace_completed"))
	})
	if relocated, err := os.Stat(recovered); err != nil || !os.SameFile(original, relocated) || trashed.Load() != 1 {
		t.Fatalf("the same entry was not moved to isolated recovery storage: requests=%d, recovered=%v", trashed.Load(), err)
	}
	assertTrashOutcome(t, w, session.Worktree, relative, "")

	w.Click(w.access(ui.RoleTreeItem, "renamed.txt"))
	w.Await("rename target was not selected", func() bool {
		item, ok := w.Tester.AccessByRole(ui.RoleTreeItem, "renamed.txt")
		return ok && item.Selected
	})
	row = w.access(ui.RoleTreeItem, "copies")
	b = row.Bounds
	w.Tester.ClickAtWith(ui.Cmd, b.X+b.W/2, b.Y+b.H/2)
	w.Await("modifier click did not preserve both selected entries", func() bool {
		file, a := w.Tester.AccessByRole(ui.RoleTreeItem, "renamed.txt")
		folder, b := w.Tester.AccessByRole(ui.RoleTreeItem, "copies")
		return a && b && file.Selected && folder.Selected
	})
	w.Tester.RightClickAt(b.X+b.W/2, b.Y+b.H/2)
	if menu := w.Tester.Menu(); slices.Contains(menu, support.Label("files_rename")) || !slices.Contains(menu, support.Label("files_trash")) {
		t.Fatalf("multi-selection's file menu: %q", menu)
	}
	if err := w.Tester.ChooseMenuItem(support.Label("files_copy_path")); err != nil {
		t.Fatal(err)
	}
	if got := w.Tester.Clipboard(); got != "docs/copies\ndocs/renamed.txt" {
		t.Fatalf("copied selected paths %q", got)
	}
}

type filesWindow struct{ *support.Window }

func openFiles(t *testing.T) filesWindow {
	t.Helper()
	w := support.Open(t, node.HarnessAssembly(), native.Assemble(files.Register))
	w.App.SetWelcome(true)
	return filesWindow{w}
}

func (w filesWindow) access(role ui.Role, label string) ui.AccessInfo {
	w.T.Helper()
	var entry ui.AccessInfo
	w.Await("missing "+label, func() bool {
		var ok bool
		entry, ok = w.Tester.AccessByRole(role, label)
		return ok && !entry.Disabled && !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0
	})
	return entry
}

func (w filesWindow) control(role ui.Role, key string) ui.AccessInfo {
	w.T.Helper()
	return w.access(role, support.Label(key))
}

func (w filesWindow) editorContains(text string) bool {
	editor, ok := w.Tester.Access("Code editor")
	return ok && strings.Contains(editor.Value, text)
}

func (w filesWindow) nameEntry(name string) {
	w.T.Helper()
	w.Click(w.control(ui.RoleTextField, "settings_name"))
	w.Tester.Key(ui.Cmd, ui.KeyA)
	w.Tester.Type(name)
	w.Tester.Key(0, ui.KeyEnter)
	w.Await("the entry form did not close", func() bool {
		_, found := w.Tester.AccessByRole(ui.RoleTextField, support.Label("settings_name"))
		return !found
	})
}
