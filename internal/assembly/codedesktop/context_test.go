package codedesktop_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestProjectContextWithCodeHost(t *testing.T) {
	w := support.Open(t, codehost.Assembly(), codedesktop.Assembly())
	if plugins := support.Result[[]protocol.PluginSummary](t, w.Do(protocol.Command{Kind: "list_plugins"})); len(plugins) != 0 {
		t.Fatalf("a fresh Code profile must have zero plugins: %+v", plugins)
	}
	project := w.CreateProject("Code workspace", t.TempDir())
	label := support.Label
	wantMenu := []string{label("workspace_project"), label("project_edit"), "-", label("workspace_new_session"), label("workspace_new_terminal"),
		label("worktree_manage"), "-", label("workspace_copy_path"), "-", label("project_remove")}
	if items := w.ProjectMenu(project.Name); !slices.Equal(items, wantMenu) {
		t.Fatalf("the Code project menu is %q, want %q", items, wantMenu)
	}
	w.Tester.CloseMenu()
	actions := w.App.Sidebar.Actions
	if actions == nil || len(actions.ProjectMenus) == 0 || !slices.ContainsFunc(actions.ProjectResources, func(action native.ProjectResourceAction) bool {
		return action.Label == "workspace_new_terminal" && action.Open != nil
	}) {
		t.Fatal("Code did not bind the project terminal and worktree adapters")
	}
	w.OpenProjectSession(project.ID)
	var labels []string
	for _, entry := range w.DetailsLauncherButtons() {
		if entry.Disabled {
			t.Errorf("the Code launcher disabled %q", entry.Name)
		}
		labels = append(labels, entry.Name)
	}
	if want := []string{label("resource_browser"), label("terminal"), label("files"), label("git")}; !slices.Equal(labels, want) {
		t.Fatalf("Code conversation launcher entries %q, want %q", labels, want)
	}
}

func TestCodeTerminalWithoutModule(t *testing.T) {
	host := codehost.Assembly()
	host.Terminal = nil
	host.Modules = slices.DeleteFunc(host.Modules, func(module node.Module) bool { return module.ID() == "terminal" })
	w := support.Open(t, host, native.Assemble(git.Register, worktrees.Register))
	project := w.CreateProject("No terminal module", t.TempDir())
	w.App.Navigate(native.ConversationPage)
	w.App.Sidebar.NewChat()
	if actions := w.App.Sidebar.Actions; actions == nil || actions.NewTerminal != nil || len(actions.ProjectResources) != 0 ||
		len(actions.ProjectMenus) == 0 || actions.ForkWorktree == nil || w.App.Panel.TerminalActions != nil {
		t.Fatal("omitting the terminal module changed unrelated actions or bound terminal actions")
	}
	items := w.ProjectMenu(project.Name)
	if slices.Contains(items, support.Label("workspace_new_terminal")) || !slices.Contains(items, support.Label("workspace_new_session")) {
		t.Fatalf("the project menu without the terminal module is %q", items)
	}
	w.Tester.CloseMenu()
	w.OpenProjectSession(project.ID)
	launcher := w.DetailsLauncherButtons()
	if len(launcher) != 1 || launcher[0].Name != support.Label("git") || launcher[0].Disabled {
		t.Fatalf("the Git-only launcher should contain only an enabled Git entry: %+v", launcher)
	}
	_, err := w.Controller.Do(w.Ctx, protocol.Command{Kind: "read_terminal_settings"})
	var fault *protocol.Fault
	if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodeNotConfigured {
		t.Fatalf("the Node without the terminal module returned %v, want not_configured", err)
	}
	if terminals := w.Controller.Snapshot().Terminals; len(terminals) != 0 || len(w.App.WorkspaceTerminalIDs()) != 0 || len(w.App.Panel.Tabs) != 0 {
		t.Fatalf("terminals appeared without the module: Node %+v, workspace %v, panel %v", terminals, w.App.WorkspaceTerminalIDs(), w.App.Panel.Tabs)
	}
}
