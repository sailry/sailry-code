package gitnode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	filesnode "github.com/sailry/sailry-harness/modules/files/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	"lukechampine.com/blake3"
)

func TestManualGitHunkAdmission(t *testing.T) {
	t.Parallel()
	assembly := native.Assembly{Modules: []native.Module{gitnode.Module(), filesnode.Module()}}
	node := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), nil, native.Options{Assembly: assembly})
	t.Cleanup(func() {
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	config := nodetest.SessionConfig(t, node, 64000, protocol.PermissionProject)
	session := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}).Data.(protocol.Session)
	write := nodetest.Dispatch(t, node, node.ID(), protocol.NewRequest(node.ID(), protocol.Command{Kind: "write_file", Data: protocol.CommandWriteFilePayload{Worktree: session.Worktree, Path: "source.txt", Text: "before\n"}}))
	if write.Fault != nil {
		t.Fatal(write.Fault)
	}
	root := ""
	for _, tree := range nodetest.Snapshot(t, node).Worktrees {
		if tree.ID == session.Worktree {
			root = tree.Path
		}
	}
	if root == "" {
		t.Fatal("captured worktree missing")
	}
	r, err := gogit.PlainInitWithOptions(root, &gogit.PlainInitOptions{InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Add("source.txt"); err != nil {
		t.Fatal(err)
	}
	commit, err := w.Commit("Initial hunk fixture", &gogit.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.test", When: time.Unix(100, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "source.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	diff := nodetest.Command(t, node, protocol.Command{Kind: "read_git_diff", Data: protocol.CommandReadGitDiffPayload{Worktree: session.Worktree, Path: "source.txt", Scope: protocol.GitDiffScopeUnstaged}}).Data.(protocol.GitDiff)
	content := nodetest.Command(t, node, protocol.Command{Kind: "read_file", Data: protocol.CommandReadFilePayload{Worktree: session.Worktree, Path: "source.txt"}}).Data.(protocol.FileContent)
	index, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	indexHash, diffHash := blake3.Sum256(index), blake3.Sum256([]byte(diff.Text))
	head, branch := commit.String(), "main"
	request := protocol.NewRequest(node.ID(), protocol.Command{Kind: "apply_git_hunk", Data: protocol.CommandApplyGitHunkPayload{Worktree: session.Worktree, Path: "source.txt", Scope: protocol.GitDiffScopeUnstaged, Operation: protocol.GitHunkStage, DiffRevision: fmt.Sprintf("%x", diffHash), ExpectedRevision: *content.Revision, ExpectedIndex: fmt.Sprintf("%x", indexHash), ExpectedHead: &head, ExpectedBranch: &branch}})
	first := nodetest.Dispatch(t, node, node.ID(), request)
	if first.Fault != nil || first.Output.Kind != "git_hunk_applied" {
		t.Fatalf("manual hunk dispatch=%#v", first)
	}
	confirmed, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "source.txt"), []byte("external\n"), 0600); err != nil {
		t.Fatal(err)
	}
	replayed := nodetest.Dispatch(t, node, node.ID(), request)
	before, _ := json.Marshal(first)
	after, _ := json.Marshal(replayed)
	if !bytes.Equal(before, after) {
		t.Fatalf("manual hunk receipt changed: %s %s", before, after)
	}
	current, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil || !bytes.Equal(confirmed, current) {
		t.Fatal("manual hunk receipt replayed the index effect")
	}
	body, err := os.ReadFile(filepath.Join(root, "source.txt"))
	if err != nil || string(body) != "external\n" {
		t.Fatal("manual hunk receipt changed external content")
	}
}
