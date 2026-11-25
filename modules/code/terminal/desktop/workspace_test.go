package desktop_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	terminal "github.com/sailry/sailry-code/modules/code/terminal/desktop"
	terminalnode "github.com/sailry/sailry-code/modules/code/terminal/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	terminalprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestProjectAndConversationTerminals(t *testing.T) {
	host := node.Assembly{Modules: []node.Module{terminalnode.Module()}}
	assembly := native.Assemble(terminal.Register)
	if len(assembly.WorkspacePages) != 0 || len(assembly.SettingsPages) != 1 || !assembly.HasPanelResource(native.Terminal) {
		t.Fatal("Terminal must contribute its panel and settings without a workspace page")
	}
	w := support.Open(t, host, assembly)
	mainPath, detailPath := t.TempDir(), t.TempDir()
	mainProject := w.CreateProject("Project terminal", mainPath)
	detailProject := w.CreateProject("Conversation terminal", detailPath)
	w.ProjectMenu(mainProject.Name)
	if err := w.Tester.ChooseMenuItem(support.Label("workspace_new_terminal")); err != nil {
		t.Fatal(err)
	}
	app := w.App
	var workspaceTerminal terminalprotocol.TerminalID
	w.Await("project right-click did not open one main terminal", func() bool {
		terminals := w.Controller.Snapshot().Terminals
		if len(terminals) != 1 || terminals[0].Status.Kind != "running" {
			return false
		}
		workspaceTerminal = terminals[0].ID
		id := workspaceTerminal.String()
		return app.Page == native.ConversationPage && slices.Equal(app.WorkspaceTerminalIDs(), []string{id}) && app.ActiveTerminal() == id && app.FocusedConversation() == "" && len(app.Panel.Tabs) == 0
	})
	var mainWorktree protocol.WorktreeID
	for _, tree := range w.Controller.Snapshot().Worktrees {
		if tree.Main && tree.Project != nil && *tree.Project == mainProject.ID {
			mainWorktree = tree.ID
			break
		}
	}
	if mainWorktree == (protocol.WorktreeID{}) {
		t.Fatal("the Node did not report the project's main worktree")
	}
	checkTerminalDirectory(w, workspaceTerminal, mainWorktree, mainPath)
	runPTY(w, workspaceTerminal, "code-project")

	session := w.OpenProjectSession(detailProject.ID)
	items := w.DetailsLauncherButtons()
	if len(items) != 1 || items[0].Name != support.Label("terminal") || items[0].Disabled {
		t.Fatalf("the conversation panel should offer one enabled Terminal item: %+v", items)
	}
	w.Click(items[0])
	var panelTerminal terminalprotocol.TerminalID
	w.Await("the conversation's Terminal entry did not open a distinct detail tab", func() bool {
		terminals := w.Controller.Snapshot().Terminals
		if len(terminals) != 2 || !app.PanelOpen || len(app.Panel.Tabs) != 1 || app.FocusedConversation() != session.ID.String() {
			return false
		}
		for _, info := range terminals {
			if info.ID != workspaceTerminal && info.Status.Kind == "running" && app.Panel.Active.Kind == native.Terminal && app.Panel.Active.ID == info.ID.String() {
				panelTerminal = info.ID
				return !app.HasWorkspaceTerminal(info.ID.String()) && slices.Equal(app.ConversationTabs(session.ID.String()), app.Panel.Tabs)
			}
		}
		return false
	})
	checkTerminalDirectory(w, panelTerminal, session.Worktree, detailPath)
	runPTY(w, panelTerminal, "code-detail")
	t.Run("LegacyCommands", func(t *testing.T) {
		legacy := *w
		legacy.T = t
		checkLegacyTerminalPackage(&legacy, mainProject.Name)
	})
}

func checkTerminalDirectory(w *support.Window, id terminalprotocol.TerminalID, worktree protocol.WorktreeID, path string) {
	w.T.Helper()
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		w.T.Fatal(err)
	}
	for _, info := range w.Controller.Snapshot().Terminals {
		if info.ID != id {
			continue
		}
		if info.Status.Kind != "running" || info.Worktree == nil || *info.Worktree != worktree || info.Directory == nil || info.Ssh != nil || info.Tool != nil {
			w.T.Fatalf("terminal %s is not a running local shell in worktree %s: %+v", id, worktree, info)
		}
		if resolved, err := filepath.EvalSymlinks(*info.Directory); err != nil || resolved != want {
			w.T.Fatalf("terminal %s started in %q (%v), want %q", id, *info.Directory, err, want)
		}
		return
	}
	w.T.Fatalf("the Node is missing terminal %s", id)
}

func runPTY(w *support.Window, id terminalprotocol.TerminalID, marker string) {
	w.T.Helper()
	body, err := json.Marshal(map[string]any{"kind": "inspect_terminal", "data": map[string]any{"terminal": id}})
	if err != nil {
		w.T.Fatal(err)
	}
	var inspect protocol.Command
	if err := json.Unmarshal(body, &inspect); err != nil {
		w.T.Fatalf("decode inspect_terminal: %v", err)
	}
	w.Await("the terminal did not take keyboard focus", func() bool { return w.Tester.Focused(support.Label("terminal")) })
	w.Tester.Type("[ -t 0 ] && [ -t 1 ] && echo " + marker + "-$((6*7))")
	w.Tester.Key(0, ui.KeyEnter)
	w.Await("the Node PTY did not execute the command typed in "+id.String(), func() bool {
		out := w.Do(inspect)
		if out.Kind != "terminal_snapshot" {
			w.T.Fatalf("inspect_terminal returned %q", out.Kind)
		}
		body, err := json.Marshal(out.Data)
		if err != nil {
			w.T.Fatal(err)
		}
		var snapshot struct {
			Screen struct {
				VT *struct{ Bytes terminalprotocol.Bytes } `json:"vt"`
			} `json:"screen"`
		}
		if err := json.Unmarshal(body, &snapshot); err != nil {
			w.T.Fatal(err)
		}
		return snapshot.Screen.VT != nil && strings.Contains(string(snapshot.Screen.VT.Bytes), marker+"-42")
	})
}
