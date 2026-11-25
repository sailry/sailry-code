package gitnode_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestGitModuleClonesProjectAndReplaysReceipt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git executable is required")
	}
	owner := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), nil, native.Options{Assembly: native.Assembly{Modules: []native.Module{gitnode.Module()}}})
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	source, _ := nodetest.GitProject(t, map[string]string{"source.txt": "local clone\n"})
	request := protocol.NewRequest(owner.ID(), protocol.Command{Kind: "create_project", Data: nativeprotocol.ProjectsDraft{
		Name: "Local clone fixture", Path: filepath.Join(t.TempDir(), "checkout"), Appearance: protocol.DefaultProjectsAppearance(),
		Source: nativeprotocol.ProjectsSource{Kind: "clone", Data: nativeprotocol.ProjectsSourceClonePayload{Url: source}},
	}})
	created, err := nodetest.Request[protocol.Project](t, owner, request, nodetest.Wait)
	if err != nil {
		t.Fatal(err)
	}
	main := nodetest.MainWorktree(t, owner, created.ID)
	path := filepath.Join(created.Path, "source.txt")
	if body, err := os.ReadFile(path); err != nil || string(body) != "local clone\n" {
		t.Fatalf("cloned file = %q, %v", body, err)
	}
	if err := os.WriteFile(path, []byte("local edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replayed, err := nodetest.Request[protocol.Project](t, owner, request, nodetest.Wait)
	if err != nil || replayed.ID != created.ID || nodetest.MainWorktree(t, owner, created.ID) != main {
		t.Fatalf("clone replay = %+v, %v", replayed, err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "local edit\n" {
		t.Fatalf("clone replay changed the workspace: %q, %v", body, err)
	}
}
