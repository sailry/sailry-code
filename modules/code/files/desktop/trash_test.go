package desktop_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
)

func TestFilesWorkspaceTrashRetainsEntryOnFault(t *testing.T) {
	for _, test := range []struct {
		code  protocol.ErrorCode
		label string
	}{
		{protocol.ErrorCodePermissionDenied, "workspace_action_failed"},
		{protocol.ErrorCodeOutcomeUnknown, "workspace_action_unknown"},
	} {
		t.Run(string(test.code), func(t *testing.T) {
			folder := t.TempDir()
			const name, body = "source.txt", "a file to keep when Trash is not confirmed"
			source := filepath.Join(folder, name)
			if err := os.WriteFile(source, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			w, recovered := openFilesWithTrash(t, folder, name, test.code)
			var trashed atomic.Int32
			w.Controller.OnDispatch(func(kind string) {
				if kind == "trash_entry" {
					trashed.Add(1)
				}
			})
			project := w.CreateProject("Trash outcome", folder)
			session := w.OpenProjectSession(project.ID)
			w.Click(w.control(ui.RoleButton, "files"))
			w.Click(w.access(ui.RoleTreeItem, name))
			w.Await("Files did not open the original document", func() bool { return w.editorContains(body) })

			row := w.access(ui.RoleTreeItem, name).Bounds
			w.Tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
			if err := w.Tester.ChooseMenuItem(support.Label("files_trash")); err != nil {
				t.Fatal(err)
			}
			confirm := w.control(ui.RoleButton, "files_trash")
			if trashed.Load() != 0 {
				t.Fatal("trash was dispatched before confirmation")
			}
			w.Click(confirm)
			w.Await("Files did not report the Node fault", func() bool { return w.Tester.HasText(support.Label(test.label)) })
			if trashed.Load() != 1 || w.Tester.HasText(support.Label("workspace_completed")) {
				t.Fatalf("a failed trash was replayed or reported as success: requests=%d, visible=%q", trashed.Load(), w.Tester.Texts())
			}
			w.Click(w.control(ui.RoleButton, "settings_cancel"))
			w.access(ui.RoleTreeItem, name)
			if !w.editorContains(body) {
				t.Fatalf("an unconfirmed trash closed the original document: %q", w.Tester.Texts())
			}
			if data, err := os.ReadFile(source); err != nil || string(data) != body {
				t.Fatalf("the unconfirmed entry was changed: %q, %v", data, err)
			}
			if _, err := os.Stat(recovered); !os.IsNotExist(err) {
				t.Fatalf("the unconfirmed entry reached recovery storage: %v", err)
			}
			assertTrashOutcome(t, w, session.Worktree, name, test.code)
		})
	}
}

func openFilesWithTrash(t *testing.T, folder, relative string, fault protocol.ErrorCode) (filesWindow, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		t.Fatal(err)
	}
	recovered := filepath.Join(t.TempDir(), filepath.Base(relative))
	assembly := node.HarnessAssembly()
	assembly.Modules = append(assembly.Modules, trashFixture{root: root, relative: relative, recovered: recovered, fault: fault})
	w := support.Open(t, assembly, native.Assemble(files.Register))
	w.App.SetWelcome(true)
	return filesWindow{w}, recovered
}

// This test-only command keeps Node admission, workspace scope and completion
// while relocating the captured fixture without consulting the user's Trash.
type trashFixture struct {
	root, relative, recovered string
	fault                     protocol.ErrorCode
}

func (trashFixture) ID() string                { return "files-trash-fixture" }
func (trashFixture) ReplacesPlugins() []string { return nil }
func (trashFixture) Catalog() []node.ToolMeta  { return nil }
func (trashFixture) ToolsForTurn(context.Context, node.Host, *node.AgentInvocation) ([]node.Tool, error) {
	return nil, nil
}

func (f trashFixture) WorkspaceServices() node.WorkspaceServices {
	return node.WorkspaceServices{Bind: func(*node.WorkspaceRuntime) (node.WorkspaceBindings, error) {
		return node.WorkspaceBindings{Commands: map[string]node.WorkspaceCommand{"trash_entry": {Run: f.trash}}}, nil
	}}
}

func (f trashFixture) trash(_ context.Context, scope node.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
	target, err := trashCommandTarget(command)
	if err != nil {
		return protocol.Output{}, err
	}
	if scope.Root() != f.root || target.Path != f.relative {
		return protocol.Output{}, fmt.Errorf("unexpected trash scope %q and entry %q", scope.Root(), target.Path)
	}
	if err := scope.ProtectEntry(target.Path); err != nil {
		return protocol.Output{}, err
	}
	if f.fault != "" {
		return protocol.Output{}, &protocol.Fault{Code: f.fault, Message: "fixture Trash did not confirm the move"}
	}
	encoded, err := json.Marshal(map[string]any{"kind": "entry_trashed", "data": trashPath{Path: target.Path}})
	if err != nil {
		return protocol.Output{}, err
	}
	var output protocol.Output
	if err := json.Unmarshal(encoded, &output); err != nil {
		return protocol.Output{}, err
	}
	if err := os.Rename(filepath.Join(f.root, filepath.FromSlash(target.Path)), f.recovered); err != nil {
		return protocol.Output{}, err
	}
	scope.Changed()
	return output, nil
}

type trashPath struct {
	Path string `json:"path"`
}

type trashTarget struct {
	Worktree protocol.WorktreeID `json:"worktree"`
	trashPath
}

func trashCommandTarget(command protocol.Command) (trashTarget, error) {
	var target trashTarget
	encoded, err := json.Marshal(command.Data)
	if err != nil {
		return target, err
	}
	err = json.Unmarshal(encoded, &target)
	return target, err
}

func assertTrashOutcome(t *testing.T, w filesWindow, worktree protocol.WorktreeID, path string, wantFault protocol.ErrorCode) {
	t.Helper()
	// Find the exact UI request for the original Node, then inspect it through
	// the Client; do not replay the command to check its effect.
	var count int
	var captured string
	caller := w.Owner.ID()
	if err := sdktest.Inspect(w.Owner).Store().Node.Read(w.Ctx, func(db *sql.Conn) error {
		return db.QueryRowContext(w.Ctx, `SELECT count(*), coalesce(max(CAST(body AS TEXT)), '')
			FROM requests WHERE caller = ? AND instr(CAST(body AS TEXT), ?) > 0`, caller[:], `"kind":"trash_entry"`).Scan(&count, &captured)
	}); err != nil || count != 1 {
		t.Fatalf("expected one admitted Trash request: count=%d, error=%v", count, err)
	}
	var request protocol.Request
	if err := json.Unmarshal([]byte(captured), &request); err != nil {
		t.Fatal(err)
	}
	target, err := trashCommandTarget(request.Command)
	if err != nil || request.Command.Kind != "trash_entry" || request.Target != caller || target.Worktree != worktree || target.Path != path {
		t.Fatalf("unexpected Trash request for worktree %v and path %q: %+v, %v", worktree, path, request, err)
	}
	resolved, err := w.Controller.Resolve(w.Ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if wantFault != "" {
		if resolved.Fault == nil || resolved.Fault.Code != wantFault || resolved.Output.Kind != "" {
			t.Fatalf("Trash receipt did not preserve the Node fault %s: %+v", wantFault, resolved)
		}
		return
	}
	var moved trashPath
	encoded, err := json.Marshal(resolved.Output.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &moved); err != nil || resolved.Fault != nil || resolved.Output.Kind != "entry_trashed" || moved.Path != path {
		t.Fatalf("Trash receipt did not confirm the captured entry: %+v, %v", resolved, err)
	}
}
