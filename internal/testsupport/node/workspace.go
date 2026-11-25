package nodetest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

// GitProject writes the fixture into a new Git repository with one commit
// and returns its root and commit.
func GitProject(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := gogit.PlainInitWithOptions(root, &gogit.PlainInitOptions{InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")}})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := tree.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := tree.Commit("Add fixture", &gogit.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.test", When: time.Unix(100, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	return root, commit.String()
}

// MainWorktree returns the main worktree of a project.
func MainWorktree(t *testing.T, node *native.Node, project protocol.ProjectID) protocol.WorktreeID {
	t.Helper()
	for _, tree := range Snapshot(t, node).Worktrees {
		if tree.Project != nil && *tree.Project == project && tree.Main {
			return tree.ID
		}
	}
	t.Fatal("project has no main worktree")
	return protocol.WorktreeID{}
}
