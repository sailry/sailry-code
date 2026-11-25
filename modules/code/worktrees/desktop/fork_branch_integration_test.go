package worktreesdesktop_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	workspaceProtocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestForkFromBranchRequiresBothModules(t *testing.T) {
	const menu = "Fork from branch"
	for _, variant := range []struct {
		name           string
		git, worktrees bool
	}{
		{name: "Git only", git: true},
		{name: "Worktrees only", worktrees: true},
		{name: "Git and Worktrees", git: true, worktrees: true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			var host node.Assembly
			var registers []native.Module
			if variant.git {
				host.Modules = append(host.Modules, gitnode.Module())
				registers = append(registers, git.Register)
			}
			if variant.worktrees {
				host.Modules = append(host.Modules, worktreesnode.Module())
				registers = append(registers, worktrees.Register)
			}
			w := support.Open(t, host, native.Assemble(registers...))
			root := worktreeRepository(t)
			mainCommit := runGit(t, root, "rev-parse", "HEAD")
			const base = "release-base"
			runGit(t, root, "switch", "-c", base)
			if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("selected branch content\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGit(t, root, "add", "note.txt")
			runGit(t, root, "-c", "user.name=Worktree Fixture", "-c", "user.email=worktrees@example.invalid", "commit", "-m", "Selected branch")
			baseCommit := runGit(t, root, "rev-parse", "HEAD")
			runGit(t, root, "switch", "main")
			project := w.CreateProject("Fork fixture", root)
			var calls atomic.Int32
			var session protocol.Session
			if variant.git && variant.worktrees {
				session = completedForkSource(t, w, project.ID, &calls)
			} else {
				session = w.OpenProjectSession(project.ID)
			}
			if !variant.git {
				if controlCount(w, ui.RoleMenuButton, "main") != 0 || w.Tester.HasText(menu) {
					t.Fatal("Worktrees alone introduced a Git branch menu")
				}
				if items := w.ProjectMenu(project.Name); !slices.Contains(items, support.Label("worktree_manage")) || slices.Contains(items, menu) {
					t.Fatalf("Worktrees-only project menu = %q, want management but not branch fork", items)
				}
				return
			}
			clickControl(t, w, ui.RoleMenuButton, "main")
			if !variant.worktrees {
				w.Await("Git-only branch menu did not offer copying", func() bool { return w.Tester.HasText(support.Label("git_copy_branch")) })
				if w.Tester.HasText(menu) {
					t.Fatal("Git-only saved session offered a worktree fork")
				}
				_, err := w.Controller.Do(w.Ctx, workspaceProtocol.Command{Kind: "create_managed_worktree", Data: workspaceProtocol.CommandCreateManagedWorktreePayload{
					Project: project.ID, Source: session.Worktree, Branch: "unexpected-write", Base: &baseCommit,
				}})
				var fault *protocol.Fault
				if !errors.As(err, &fault) || fault.Code != workspaceProtocol.ErrorCodeNotConfigured {
					t.Fatalf("Git-only Node did not refuse worktree writes: %v", err)
				}
				if len(w.Controller.Snapshot().Worktrees) != 1 || strings.Contains(runGit(t, root, "worktree", "list", "--porcelain"), "unexpected-write") {
					t.Fatal("Git-only command created a worktree")
				}
				return
			}
			var submenu ui.Rect
			w.Await("the saved session has no branch fork submenu", func() bool {
				var ok bool
				submenu, ok = w.Tester.Find(menu)
				return ok
			})
			w.Tester.Move(submenu.X+submenu.W/2, submenu.Y+submenu.H/2)
			w.Tester.Advance(200 * time.Millisecond)
			clickMenuChoice(t, w, base)
			w.Await("the fork prompt did not identify its source branch", func() bool {
				return w.Tester.HasText("Source branch") && w.Tester.HasText(base) && controlCount(w, ui.RoleTextField, support.Label("git_new_branch")) == 1
			})
			if controlCount(w, ui.RoleCheckBox, support.Label("location_copy_changes")) != 0 {
				t.Fatal("fork from another commit offered to copy local changes")
			}
			if len(w.Controller.Snapshot().Worktrees) != 1 || len(w.Controller.Snapshot().Sessions) != 1 {
				t.Fatal("selecting a source branch performed a write before confirmation")
			}
			if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("uncommitted source changes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGit(t, root, "branch", "-f", base, mainCommit)
			clickControl(t, w, ui.RoleButton, support.Label("location_fork"))
			var source, forked protocol.Session
			var checkout protocol.Worktree
			w.Await("the fork and its worktree were not created and selected", func() bool {
				state := w.Controller.Snapshot()
				if len(state.Sessions) != 2 || len(state.Worktrees) != 2 {
					return false
				}
				for _, candidate := range state.Sessions {
					if candidate.ID == session.ID {
						source = candidate
					} else {
						forked = candidate
					}
				}
				for _, candidate := range state.Worktrees {
					if candidate.ID == forked.Worktree && !candidate.Main {
						checkout = candidate
					}
				}
				return checkout.Path != "" && w.App.Sidebar.Selected == forked.ID.String() && w.App.FocusedConversation() == forked.ID.String()
			})
			if source.Worktree != session.Worktree || source.Revision != session.Revision || checkout.Project == nil || *checkout.Project != project.ID ||
				forked.Fork == nil || forked.Fork.Session != source.ID {
				t.Fatalf("unexpected worktree scopes: source=%+v fork=%+v checkout=%+v", source, forked, checkout)
			}
			if got := runGit(t, checkout.Path, "rev-parse", "HEAD"); got != baseCommit {
				t.Fatalf("fork HEAD = %s, want the selected commit %s (original branch now points to %s)", got, baseCommit, mainCommit)
			}
			if branch := runGit(t, checkout.Path, "branch", "--show-current"); !strings.HasPrefix(branch, "sailry/task-") || runGit(t, root, "branch", "--show-current") != "main" {
				t.Fatalf("fork or source checkout switched unexpectedly: fork branch = %q", branch)
			}
			for path, expected := range map[string]string{root: "uncommitted source changes\n", checkout.Path: "selected branch content\n"} {
				if data, err := os.ReadFile(filepath.Join(path, "note.txt")); err != nil || string(data) != expected {
					t.Fatalf("%s contains %q (%v), want %q", path, data, err, expected)
				}
			}
			w.Await("the saved history was not projected in the fork", func() bool { return w.Tester.HasText("History fixture answer") })
			original := readForkHistory(t, w, source.ID)
			copied := readForkHistory(t, w, forked.ID)
			if len(original.Page.Runs) != 1 || len(copied.Page.Runs) != 1 || original.Page.Runs[0].Status != protocol.ConversationStatusCompleted ||
				copied.Page.Runs[0].Status != protocol.ConversationStatusCompleted || copied.Page.Runs[0].Turn != original.Page.Runs[0].Turn ||
				forked.Fork.Through != original.Page.Runs[0].Turn ||
				len(copied.Page.Queue.Items) != 0 || calls.Load() != 1 {
				t.Fatalf("fork did not copy history without replay: original=%+v fork=%+v model requests=%d", original.Page.Runs, copied.Page.Runs, calls.Load())
			}
		})
	}
}

func completedForkSource(t *testing.T, w *support.Window, project protocol.ProjectID, calls *atomic.Int32) protocol.Session {
	t.Helper()
	const key = "fixture-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected model request: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if request.Header.Get("Authorization") != "Bearer "+key {
			t.Error("the turn did not use the source Node's fixture credential")
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, `data: {"id":"fixture","object":"chat.completion.chunk","model":"fixture","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":"History fixture answer"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(writer, `data: {"id":"fixture","object":"chat.completion.chunk","model":"fixture","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Fork model fixture", true
	provider.Api, provider.Authentication, provider.Endpoint = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, server.URL+"/v1"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 32000, 8000
	provider.DefaultModel, provider.Models = model.ID, []protocol.ConversationModel{model}
	secret := protocol.NewSecret(key)
	saved := support.Result[protocol.ConversationProvider](t, w.Do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}}))
	if saved.Credential == nil || saved.Credential.Node != w.Owner.ID() {
		t.Fatal("provider receipt did not bind a credential to the source Node")
	}
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Credential, config.Effort = saved.ID, saved.DefaultModel, saved.Credential, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionFull
	session := support.Result[protocol.Session](t, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project, Config: &config}}))
	if session.Project == nil || *session.Project != project || session.Worktree == (protocol.WorktreeID{}) {
		t.Fatalf("source session %s is not in the fixture project and worktree", session.ID)
	}
	// Prepare the history on the source Node before opening the composer, whose
	// editable model selection is not part of the branch-fork workflow.
	changed := make(chan struct{}, 1)
	release, err := w.Controller.WatchConversation(w.Ctx, session.ID, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	queued := support.Result[protocol.QueuedTurn](t, w.Do(protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
		Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Keep this saved turn"},
	}}))
	if queued.Session != session.ID || queued.ID == (protocol.TurnID{}) || queued.Config.Provider != config.Provider || queued.Config.Model != config.Model ||
		queued.Config.Permission != config.Permission || queued.Config.Credential == nil || *queued.Config.Credential != *config.Credential {
		t.Fatalf("turn %s was not admitted with the saved source session and model", queued.ID)
	}
	awaitSourceTurn(t, w, session, queued.ID, changed)
	history := readForkHistory(t, w, session.ID)
	if len(history.Page.Runs) != 1 || history.Page.Runs[0].Turn != queued.ID || history.Page.Runs[0].Status != protocol.ConversationStatusCompleted ||
		len(history.Page.Queue.Items) != 0 || calls.Load() != 1 {
		t.Fatalf("source conversation has unexpected saved history: runs=%+v queue=%+v model requests=%d", history.Page.Runs, history.Page.Queue.Items, calls.Load())
	}

	w.OpenSession(session.ID.String())
	w.Await("the saved source conversation did not project its history", func() bool {
		id := session.ID.String()
		if w.App.Page != native.ConversationPage || w.App.Sidebar.Selected != id || w.App.FocusedConversation() != id || !w.Tester.HasText("History fixture answer") {
			return false
		}
		for _, current := range w.Controller.Snapshot().Sessions {
			if current.ID == session.ID {
				session = current
				return true
			}
		}
		return false
	})
	return session
}

func awaitSourceTurn(t *testing.T, w *support.Window, session protocol.Session, turn protocol.TurnID, changed <-chan struct{}) {
	t.Helper()
	for {
		state, _ := w.Controller.Conversation(session.ID)
		for _, run := range state.Snapshot.Page.Runs {
			if run.Turn != turn {
				continue
			}
			if run.Session != session.ID || run.Worktree != session.Worktree || run.Origin != nil {
				t.Fatalf("source turn ran outside its own session or worktree: %+v", run)
			}
			switch run.Status {
			case protocol.ConversationStatusCompleted:
				return
			case protocol.ConversationStatusFailed, protocol.ConversationStatusCancelled, protocol.ConversationStatusInterrupted:
				t.Fatalf("source turn %s ended as %s: %v", run.Turn, run.Status, run.Error)
			}
		}
		select {
		case <-changed:
		case <-w.Ctx.Done():
			t.Fatalf("source turn %s did not finish: connected=%t observation error=%v: %v", turn, state.Connected, state.Err, w.Ctx.Err())
		}
	}
}

func readForkHistory(t *testing.T, w *support.Window, id protocol.SessionID) protocol.ConversationHistory {
	t.Helper()
	return support.Result[protocol.ConversationHistory](t, w.Do(protocol.Command{Kind: "read_conversation", Data: workspaceProtocol.CommandReadConversationPayload{Session: id, Limit: 10}}))
}
