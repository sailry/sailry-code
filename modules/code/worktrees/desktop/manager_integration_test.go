package worktreesdesktop_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	workspaceProtocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestWorktreeManagerCreatesAndUsesLocation(t *testing.T) {
	const action = "Create and use worktree"
	for _, existing := range []bool{false, true} {
		name := "fresh draft"
		if existing {
			name = "existing session"
		}
		t.Run(name, func(t *testing.T) {
			w := support.Open(t, worktreeHost(), native.Assemble(git.Register, worktrees.Register))
			root := worktreeRepository(t)
			var session protocol.Session
			if !existing {
				runGit(t, root, "worktree", "add", "-b", "already-linked", filepath.Join(filepath.Dir(root), "already-linked"))
			}
			project := w.CreateProject("Create and use fixture", root)
			if existing {
				session = w.OpenProjectSession(project.ID)
				launcher := w.DetailsLauncherButtons()
				index := slices.IndexFunc(launcher, func(entry ui.AccessInfo) bool { return entry.Name == support.Label("git") })
				if index == -1 || launcher[index].Disabled {
					t.Fatalf("the existing session has no Git launcher: %+v", launcher)
				}
				w.Click(launcher[index])
				clickControl(t, w, ui.RoleMenuButton, support.Label("git_actions"))
				clickMenuChoice(t, w, support.Label("worktree_manage"))
			} else {
				w.Await("the external worktree was not listed for the project", func() bool {
					for _, current := range w.App.Data.Projects {
						if current.ID == project.ID.String() {
							return len(current.Worktrees) == 2
						}
					}
					return false
				})
				w.ProjectMenu(project.Name)
				if err := w.Tester.ChooseMenuItem(support.Label("worktree_manage"), support.Label("worktree_remove")); err != nil {
					t.Fatal(err)
				}
			}
			clickControl(t, w, ui.RoleButton, action)
			conversation := w.App.Sidebar.Selected
			if existing && conversation != session.ID.String() || !existing && !strings.HasPrefix(conversation, "draft/") {
				t.Fatalf("the manager was opened from the wrong conversation: %q", conversation)
			}
			composer := w.App.Composer(conversation)
			composer.Draft = "Keep this draft"
			target := filepath.Join(filepath.Dir(root), "created-and-used")
			clickControl(t, w, ui.RoleTextField, support.Label("git_new_branch"))
			w.Tester.Type("created-and-used")
			clickControl(t, w, ui.RoleTextField, support.Label("worktree_path"))
			w.Tester.Type(target)
			clickControl(t, w, ui.RoleButton, action)
			w.Await("the new worktree did not become the captured conversation's location", func() bool {
				snapshot := w.Controller.Snapshot()
				var created protocol.WorktreeID
				for _, tree := range snapshot.Worktrees {
					if tree.Path == target && tree.Project != nil && *tree.Project == project.ID {
						created = tree.ID
					}
				}
				if created == (protocol.WorktreeID{}) || w.App.Sidebar.Selected != conversation {
					return false
				}
				if !existing {
					return composer.Worktree == target && len(snapshot.Sessions) == 0
				}
				projected := w.App.Data.Find(conversation)
				return projected != nil && projected.WorktreePath == target && slices.ContainsFunc(snapshot.Sessions, func(current protocol.Session) bool {
					return current.ID == session.ID && current.Revision > session.Revision && current.Worktree == created
				})
			})
			if composer.Draft != "Keep this draft" || runGit(t, target, "branch", "--show-current") != "created-and-used" {
				t.Fatal("creating and using a worktree lost conversation input or checked out the wrong branch")
			}
		})
	}
}

func TestWorktreeManagerChangesAndRemovalGuards(t *testing.T) {
	assembly := native.Assemble(worktrees.Register)
	var bound native.ProjectWorkflowHost
	bind := assembly.ProjectWorkflows[0]
	assembly.ProjectWorkflows[0] = func(host native.ProjectWorkflowHost) { bound = host; bind(host) }
	git.Register(&assembly)
	w := support.Open(t, worktreeHost(), assembly)
	root := worktreeRepository(t)
	parent := filepath.Dir(root)
	unregistered := filepath.Join(parent, "outside")
	locked := filepath.Join(parent, "locked")
	stale := filepath.Join(parent, "stale")
	removable := filepath.Join(parent, "removable")
	for _, path := range []string{unregistered, locked, stale, removable} {
		runGit(t, root, "worktree", "add", "-b", filepath.Base(path), path)
	}
	runGit(t, root, "worktree", "lock", locked)
	if err := os.RemoveAll(stale); err != nil {
		t.Fatal(err)
	}
	project := w.CreateProject("Manager fixture", root)
	var listed *native.ProjectMenuProject
	w.Await("the project was not projected", func() bool {
		for _, entry := range w.App.Data.Projects {
			if entry.ID == project.ID.String() {
				listed = entry
				return true
			}
		}
		return false
	})
	draft, ok := bound.NewDraft(listed)
	if !ok {
		t.Fatal("could not open a draft scoped to the fixture project")
	}
	workspace, ok := draft.OpenWorkspace()
	if !ok || workspace.Worktree.Path != root {
		t.Fatalf("wrong draft workspace: %+v", workspace.Worktree)
	}
	composition := w.App.Composer(w.App.Sidebar.Selected)
	composition.Draft = "Keep my draft"
	var rows []native.WorkspacePickerRow
	var confirmed func(func(bool))
	var form func([]string, func(bool))
	var result []bool
	var reports []string
	workspace.Pick = func(_ string, value []native.WorkspacePickerRow) { rows = value }
	workspace.Prompt = func(_ string, _, _ []string, submit func([]string, func(bool)), _ ...native.WorkspacePromptToggle) {
		form = submit
	}
	workspace.Confirm = func(_ string, target string, submit func(func(bool)), _ ...string) {
		if filepath.Dir(target) != parent {
			t.Fatalf("confirmation target outside the fixture: %q", target)
		}
		confirmed = submit
	}
	workspace.Report = func(_ *ui.Context, _ error, key, _, _ string) { reports = append(reports, key) }
	choose := func(label string) {
		t.Helper()
		for _, row := range rows {
			if row.Label == label && !row.Disabled && row.Run != nil {
				ui.NewTester(row.Run, 480, 320).Frame()
				return
			}
		}
		t.Fatalf("picker row %q is absent: %+v", label, rows)
	}
	list := func() {
		rows = nil
		worktrees.Manage(workspace)
		w.Await("the manager did not receive the Git worktrees", func() bool { return len(rows) > 0 })
	}
	check := func(want bool) {
		t.Helper()
		if confirmed == nil {
			t.Fatal("the change was not confirmed before its request")
		}
		prior := len(result)
		confirmed(func(ok bool) { result = append(result, ok) })
		w.Await("the worktree request did not settle", func() bool { return len(result) == prior+1 })
		if result[prior] != want {
			t.Fatalf("confirmation result %v, want %v (reports: %q)", result[prior], want, reports)
		}
		confirmed = nil
	}

	list()
	choose("locked")
	if slices.ContainsFunc(rows, func(row native.WorkspacePickerRow) bool { return row.Label == support.Label("worktree_remove") }) {
		t.Fatal("the manager offered deletion of a locked worktree")
	}
	head := runGit(t, root, "rev-parse", "HEAD")
	lockRecord := support.Result[protocol.Worktree](t, w.Do(workspaceProtocol.Command{Kind: "register_worktree", Data: workspaceProtocol.CommandRegisterWorktreePayload{Project: project.ID, Path: locked}}))
	_, err := w.Controller.Do(w.Ctx, workspaceProtocol.Command{Kind: "remove_worktree", Data: workspaceProtocol.CommandRemoveWorktreePayload{Worktree: lockRecord.ID, ExpectedHead: head, ExpectedBranch: "locked"}})
	fault, ok := errors.AsType[protocol.Fault](err)
	if !ok || fault.Code != workspaceProtocol.ErrorCodeBusy || fault.Message != "worktree is locked" {
		t.Fatalf("node did not protect the locked worktree: %v", err)
	}

	list()
	choose("removable")
	choose(support.Label("worktree_remove"))
	if _, err := os.Stat(removable); err != nil {
		t.Fatalf("the unregistered checkout changed before confirmation: %v", err)
	}
	check(true)
	if _, err := os.Stat(removable); !os.IsNotExist(err) || runGit(t, root, "rev-parse", "removable") != head {
		t.Fatalf("removal did not delete only the checkout directory: %v", err)
	}

	list()
	choose("stale")
	check(true)
	w.Await("the pruned checkout was still listed", func() bool {
		return len(rows) > 0 && !slices.ContainsFunc(rows, func(row native.WorkspacePickerRow) bool { return row.Label == "stale" })
	})
	if worktrees := runGit(t, root, "worktree", "list", "--porcelain"); slices.Contains(strings.Split(worktrees, "\n"), "worktree "+stale) {
		t.Fatalf("stale Git worktree registration remains: %s", worktrees)
	}

	ui.NewTester(func(c *ui.Context) { worktrees.CreateAt(c, workspace, "main", head) }, 480, 320).Frame()
	if form == nil {
		t.Fatal("the explicit checkout form did not open")
	}
	created := filepath.Join(parent, "created")
	prior := len(result)
	form([]string{" main ", " created ", " " + created + " "}, func(ok bool) { result = append(result, ok) })
	w.Await("the explicit worktree creation did not settle", func() bool { return len(result) == prior+1 })
	if !result[prior] || runGit(t, created, "branch", "--show-current") != "created" || runGit(t, created, "rev-parse", "HEAD") != head {
		t.Fatalf("the requested branch and commit were not checked out: %v, reports %q", result, reports)
	}
	if composition.Worktree != root || composition.Draft != "Keep my draft" {
		t.Fatalf("the create-only action changed the draft location or contents: %q / %q", composition.Worktree, composition.Draft)
	}

	if err := os.WriteFile(filepath.Join(created, "note.txt"), []byte("local edits\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	list()
	choose("created")
	choose(support.Label("worktree_remove"))
	check(false)
	if !slices.Contains(reports, "worktree_remove_dirty") {
		t.Fatalf("dirty removal reports %q", reports)
	}
	if contents, err := os.ReadFile(filepath.Join(created, "note.txt")); err != nil || string(contents) != "local edits\n" {
		t.Fatalf("dirty removal lost work: %q, %v", contents, err)
	}
	runGit(t, created, "restore", "note.txt")

	listedCreate := func() protocol.WorktreeID {
		t.Helper()
		for _, tree := range w.Controller.Snapshot().Worktrees {
			if tree.Path == created {
				return tree.ID
			}
		}
		return protocol.WorktreeID{}
	}
	w.Await("the created checkout did not enter the Node catalog", func() bool { return listedCreate() != (protocol.WorktreeID{}) })
	startBlockingWorktreeTurn(t, w, project.ID, listedCreate())
	list()
	choose("created")
	choose(support.Label("worktree_remove"))
	check(false)
	if !slices.Contains(reports, "worktree_remove_busy") {
		t.Fatalf("active checkout removal reports %q", reports)
	}
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("active worktree directory disappeared: %v", err)
	}

	list()
	choose("outside")
	choose(support.Label("workspace_open_worktree"))
	w.Await("selection did not register and apply the external checkout", func() bool {
		return composition.Worktree == unregistered && slices.ContainsFunc(w.Controller.Snapshot().Worktrees, func(tree protocol.Worktree) bool { return tree.Path == unregistered })
	})
	if composition.Draft != "Keep my draft" {
		t.Fatal("opening the external checkout lost the draft or did not register the path")
	}
}

func startBlockingWorktreeTurn(t *testing.T, w *support.Window, project protocol.ProjectID, tree protocol.WorktreeID) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	notify := sync.OnceFunc(func() { close(started) })
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		notify()
		select {
		case <-request.Context().Done():
		case <-release:
			response.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(func() { close(release); server.Close() })
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Blocking worktree fixture", true
	provider.Api, provider.Authentication, provider.Endpoint = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, server.URL
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 32000, 8000
	provider.DefaultModel, provider.Models = model.ID, []protocol.ConversationModel{model}
	secret := protocol.NewSecret("fixture-key")
	w.Do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Effort = provider.ID, model.ID, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionFull
	session := support.Result[protocol.Session](t, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project, Worktree: &tree, Config: &config}}))
	w.Do(protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Hold the checkout until the test finishes"}}})
	w.Await("the running turn did not reach its local model fixture", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})
}
