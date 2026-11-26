package worktreesnode_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestAgentManagedWorktreeCreation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git executable is required")
	}
	const file, committed, changed = "source.txt", "committed\n", "uncommitted\n"
	root, head := nodetest.GitProject(t, map[string]string{file: committed})
	worktreeGit(t, root, "config", "--local", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(root, file), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	script := nodetest.NewScript()
	owner := startNode(t, native.Assembly{Modules: []native.Module{gitnode.Module(), worktreesnode.Module()}}, script)
	project := registerProject(t, owner, root)
	call := func(tool string, args any, approve func(protocol.ConversationApproval)) map[string]*sdktest.ToolResponse {
		t.Helper()
		script.Reset(
			nodetest.Step{ID: "discover", Tool: "tool_search", Arguments: nodetest.Arguments(map[string]string{"query": "select:" + tool})},
			nodetest.Step{ID: "tool", Tool: tool, Arguments: nodetest.Arguments(args)},
			nodetest.Step{ID: "answer"},
		)
		session := nodetest.ProjectSession(t, owner, project.ID, protocol.PermissionAsk)
		_, results := nodetest.Turn(t, owner, session, "Run "+tool, nodetest.Wait, approve)
		return results
	}
	read := func(path string) []byte {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	approve := func(protocol.ConversationApproval) {}
	assertCreated := func(results map[string]*sdktest.ToolResponse, want string) {
		t.Helper()
		value := nodetest.Result(t, results, "tool", "create_worktree")
		data, _ := value["data"].(map[string]any)
		id, _ := data["id"].(string)
		path, _ := data["path"].(string)
		if value["kind"] != "worktree" || id == "" || path != filepath.Join(root, ".sailry", "worktrees", id) {
			t.Fatalf("expected a registered managed worktree, got %v", value)
		}
		if got := string(read(filepath.Join(path, file))); got != want {
			t.Fatalf("created checkout content = %q, want %q", got, want)
		}
	}

	assertCreated(call("create_worktree", map[string]string{"branch": "agent-clean"}, approve), committed)
	results := call("git_status", map[string]any{}, nil)
	value := nodetest.Result(t, results, "tool", "git_status")
	status, _ := value["data"].(map[string]any)
	observedHead, _ := status["head"].(string)
	index, _ := status["index_revision"].(string)
	if value["kind"] != "git_status" || observedHead != head || index == "" {
		t.Fatalf("git_status did not return the observed HEAD and index revision: %v", value)
	}
	if view, _ := script.Observation("tool"); !strings.Contains(view, observedHead) || !strings.Contains(view, index) {
		t.Fatalf("Git revisions did not reach the model: %q", view)
	}
	copyArgs := func(branch string) map[string]any {
		return map[string]any{"branch": branch, "include_changes": true, "expected_head": observedHead, "expected_index": index}
	}
	assertCreated(call("create_worktree", copyArgs("agent-copy"), approve), changed)

	assertRejected := func(branch string, results map[string]*sdktest.ToolResponse, code protocol.ErrorCode, prefix string) {
		t.Helper()
		response := results["tool"]
		if response == nil || response.Status != sdktest.ToolErrorStatus {
			t.Fatalf("create_worktree %q returned %+v, want %s", branch, response, code)
		}
		var failure struct {
			Data protocol.Fault `json:"data"`
		}
		if err := json.Unmarshal(response.Result.Value, &failure); err != nil || failure.Data.Code != code || !strings.HasPrefix(failure.Data.Message, prefix) {
			t.Fatalf("create_worktree %q returned %s, want %s (%v)", branch, response.Result.Value, code, err)
		}
		repo, err := gogit.PlainOpen(root)
		if err != nil {
			t.Fatal(err)
		}
		if ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), false); !errors.Is(err, plumbing.ErrReferenceNotFound) {
			t.Fatalf("rejected worktree left a branch: %v, %v", ref, err)
		}
	}
	registered := len(nodetest.Snapshot(t, owner).Worktrees)
	missing := copyArgs("agent-missing")
	delete(missing, "expected_index")
	assertRejected("agent-missing", call("create_worktree", missing, approve), protocol.ErrorCodeInvalidRequest, "copying source changes requires")

	var stagedIndex []byte
	indexPath := filepath.Join(root, ".git", "index")
	stale := call("create_worktree", copyArgs("agent-stale"), func(protocol.ConversationApproval) {
		worktreeGit(t, root, "add", "--", file)
		stagedIndex = read(indexPath)
	})
	assertRejected("agent-stale", stale, protocol.ErrorCodeRevisionConflict, "Git branch, HEAD, or index changed")
	if entries, err := os.ReadDir(filepath.Join(root, ".sailry", "worktrees")); err != nil || len(entries) != 2 || len(nodetest.Snapshot(t, owner).Worktrees) != registered {
		t.Fatalf("rejected calls changed the managed checkouts: %v, %v", entries, err)
	}
	if !bytes.Equal(read(indexPath), stagedIndex) || string(read(filepath.Join(root, file))) != changed {
		t.Fatal("stale worktree creation modified the source")
	}
}
