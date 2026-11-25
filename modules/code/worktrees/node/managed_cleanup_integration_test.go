//go:build integration

package worktreesnode_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	"lukechampine.com/blake3"
)

func TestWorktreesOnlyManagedSessionCleanup(t *testing.T) {
	f := newManagedFixture(t, worktreesnode.Module())
	clean, dirty, shared := f.managed(t, "clean"), f.managed(t, "dirty"), f.managed(t, "shared")
	cleanSession, dirtySession, first, second := f.session(t, clean), f.session(t, dirty), f.session(t, shared), f.session(t, shared)
	if err := os.WriteFile(filepath.Join(dirty.Path, "draft.txt"), []byte("uncommitted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.removeSessions(t, first, dirtySession, cleanSession)
	awaitManagedCleanup(t, "clean checkout removal", func() bool { return f.gone(t, clean) })
	if body, err := os.ReadFile(filepath.Join(dirty.Path, "draft.txt")); err != nil || string(body) != "uncommitted\n" || !registeredWorktree(t, f.owner, dirty.ID) {
		t.Fatalf("uncommitted work was not kept: %q, %v", body, err)
	}
	if _, err := os.Stat(shared.Path); err != nil || !registeredWorktree(t, f.owner, shared.ID) {
		t.Fatalf("a checkout still used by another session was removed: %v", err)
	}
	worktreeGit(t, f.root, "rev-parse", "--verify", "refs/heads/clean")
	f.removeSessions(t, second)
	awaitManagedCleanup(t, "last session's checkout removal", func() bool { return f.gone(t, shared) })
}

func TestManagedWorktreeSubmoduleProtection(t *testing.T) {
	f := newManagedFixture(t, worktreesnode.Module(), gitnode.Module())
	protected, clean := f.managed(t, "submodules"), f.managed(t, "batch-clean")
	module, _ := nodetest.GitProject(t, map[string]string{"file": "module contents"})
	worktreeGit(t, protected.Path, "clone", "--no-local", "--", module, "module")
	worktreeGit(t, protected.Path, "submodule", "add", "--", module, "module")
	worktreeGit(t, protected.Path, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-m", "Add submodule")
	f.removeSessions(t, f.session(t, protected), f.session(t, protected), f.session(t, clean))
	attempts := func() int {
		output := nodetest.Do[nativeprotocol.OutputGitOutputPayload](t, f.owner, protocol.Command{Kind: "read_git_output", Data: nativeprotocol.CommandReadGitOutputPayload{Worktree: f.main}})
		return strings.Count(output.Text, "> git worktree remove -- "+protected.Path+"\n")
	}
	awaitManagedCleanup(t, "clean checkout removed and submodule checkout attempted", func() bool { return f.gone(t, clean) && attempts() > 0 })
	if got := attempts(); got != 1 || !registeredWorktree(t, f.owner, protected.ID) {
		t.Fatalf("shared submodule checkout: %d auto-removal attempts, registered=%t", got, registeredWorktree(t, f.owner, protected.ID))
	}
	status := nodetest.Do[nativeprotocol.GitStatus](t, f.owner, protocol.Command{Kind: "inspect_git", Data: nativeprotocol.CommandInspectGitPayload{Worktree: protected.ID}})
	if status.Head == nil || status.Branch == nil {
		t.Fatalf("protected checkout has no branch or HEAD: %+v", status)
	}
	manual := protocol.Command{Kind: "remove_worktree", Data: protocol.CommandRemoveWorktreePayload{Worktree: protected.ID, ExpectedHead: *status.Head, ExpectedBranch: *status.Branch}}
	_, failure := nodetest.Call[json.RawMessage](t, f.owner, manual)
	if failure == nil || failure.Code != nativeprotocol.ErrorCodeConflict || !strings.Contains(failure.Message, "submodules") {
		t.Fatalf("manual removal must report Git's submodule protection: %v", failure)
	}
	modulePath := filepath.Join(protected.Path, "module")
	if body, err := os.ReadFile(filepath.Join(modulePath, "file")); err != nil || string(body) != "module contents" {
		t.Fatalf("submodule checkout was not preserved: %q, %v", body, err)
	}
	if err := os.Rename(modulePath, filepath.Join(t.TempDir(), "preserved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(modulePath, 0o700); err != nil {
		t.Fatal(err)
	}
	nodetest.Command(t, f.owner, manual)
	if got := attempts(); got != 3 || !f.gone(t, protected) {
		t.Fatalf("manual removal after preserving the submodule: %d total attempts, registered=%t", got, registeredWorktree(t, f.owner, protected.ID))
	}
}

type managedFixture struct {
	owner        *native.Node
	project      protocol.ProjectID
	main         protocol.WorktreeID
	root, commit string
	config       protocol.SessionConfig
}

func newManagedFixture(t *testing.T, modules ...native.Module) managedFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git executable is required")
	}
	owner := startNode(t, native.Assembly{Modules: modules}, nil)
	root, commit := nodetest.GitProject(t, map[string]string{"file.txt": "one\n"})
	project := registerProject(t, owner, root).ID
	return managedFixture{owner: owner, root: root, commit: commit, project: project, main: nodetest.MainWorktree(t, owner, project), config: nodetest.SessionConfig(t, owner, 200000, protocol.PermissionAsk)}
}

func (f managedFixture) managed(t *testing.T, branch string) protocol.Worktree {
	t.Helper()
	index, err := os.ReadFile(filepath.Join(f.root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	checksum := blake3.Sum256(index)
	tree := nodetest.Do[protocol.Worktree](t, f.owner, protocol.Command{Kind: "create_managed_worktree", Data: nativeprotocol.CommandCreateManagedWorktreePayload{
		Project: f.project, Source: f.main, Branch: branch, ExpectedHead: f.commit, ExpectedIndex: hex.EncodeToString(checksum[:]),
	}})
	if tree.Path != filepath.Join(f.root, ".sailry", "worktrees", tree.ID.String()) {
		t.Fatalf("managed checkout location = %s", tree.Path)
	}
	return tree
}

func (f managedFixture) session(t *testing.T, tree protocol.Worktree) protocol.Session {
	t.Helper()
	return nodetest.Do[protocol.Session](t, f.owner, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &f.project, Worktree: &tree.ID, Config: &f.config}})
}

func (f managedFixture) removeSessions(t *testing.T, sessions ...protocol.Session) {
	t.Helper()
	expected := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		expected = append(expected, map[string]any{"session": session.ID, "revision": session.Revision})
	}
	nodetest.Command(t, f.owner, nodetest.DecodeCommand(t, "remove_sessions", map[string]any{"expected": expected}))
}

func (f managedFixture) gone(t *testing.T, tree protocol.Worktree) bool {
	t.Helper()
	_, err := os.Lstat(tree.Path)
	return errors.Is(err, os.ErrNotExist) && !registeredWorktree(t, f.owner, tree.ID)
}

func awaitManagedCleanup(t *testing.T, label string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(nodetest.Wait)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", label)
		case <-ticker.C:
		}
	}
}
