package desktop_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestFilesUnassignedSessionListsEmptyDirectoryOnce(t *testing.T) {
	w := openFiles(t)
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Unassigned Files fixture", true
	provider.Api, provider.Authentication, provider.Endpoint = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://127.0.0.1:9/v1"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 32000, 8000
	provider.DefaultModel, provider.Models = model.ID, []protocol.ConversationModel{model}
	secret := protocol.NewSecret("fixture-key")
	w.Do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Effort = provider.ID, model.ID, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionFull
	session := support.Result[protocol.Session](t, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}))
	if session.Project != nil || session.Worktree == (protocol.WorktreeID{}) {
		t.Fatalf("expected an unassigned session with its own workspace: %+v", session)
	}
	w.OpenSession(session.ID.String())
	var workspace *native.Project
	w.Await("the conversation did not resolve its own workspace", func() bool {
		workspace = w.App.Hooks.WorkspaceProject()
		return w.App.Page == native.ConversationPage && w.App.FocusedConversation() == session.ID.String() && workspace != nil && workspace.ID == session.Worktree.String()
	})
	var dispatched atomic.Int32
	w.Controller.OnDispatch(func(kind string) {
		if kind == "list_directory" {
			dispatched.Add(1)
		}
	})

	// A real workspace subscription sends an initial invalidation. Deliver it
	// before showing the tree so this test measures an unchanged directory.
	release, err := w.Controller.ObserveWorkspace(w.Ctx, session.Worktree, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	watching := func() bool {
		state, found := w.Controller.Workspace(session.Worktree, false)
		return found && state.Connected && state.Err == nil
	}
	w.Await("the Node did not deliver its initial workspace observation", watching)
	if dispatched.Load() != 0 {
		t.Fatal("the workspace was listed before Files was opened")
	}

	var reads, observers int
	host := workspace.FileDirectory
	workspace.FileDirectory.Observe = func(ctx context.Context) func() {
		observers++
		return host.Observe(ctx)
	}
	workspace.FileDirectory.Read = func(ctx context.Context, path string, after *protocol.DirectoryCursor, done func(*ui.Context, protocol.Directory, error)) {
		reads++
		if path != "" || after != nil {
			t.Errorf("the first worktree listing requested path %q, cursor %+v", path, after)
		}
		host.Read(ctx, path, after, done)
	}
	w.App.Navigate(native.FilesPage)
	w.App.RevealDetails()
	empty := support.Label("files_directory_empty")
	w.Await("the real Files page did not show the empty session directory", func() bool {
		return w.App.Page == native.FilesPage && w.Tester.HasText(empty)
	})
	release()
	for range 24 {
		w.Tester.Advance(time.Second / 60)
		w.Tester.Frame()
	}
	if sent := dispatched.Load(); reads != 1 || sent != 1 || observers != 1 || !watching() || !w.Tester.HasText(empty) {
		t.Fatalf("unchanged empty Files page: %d root reads, %d Node listings, %d observers, connected=%t, visible=%q",
			reads, sent, observers, watching(), w.Tester.Texts())
	}
}
