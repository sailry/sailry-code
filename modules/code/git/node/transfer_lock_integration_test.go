//go:build integration && unix

package gitnode_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestGitTransferDoesNotHoldWorkspaceEffects(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git executable is required")
	}
	options := native.Options{Assembly: native.Assembly{Modules: []native.Module{gitnode.Module()}}}
	owner := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), nil, options)
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	root, head := nodetest.GitProject(t, map[string]string{"source.txt": "fixture\n"})
	bare := t.TempDir()
	git := func(root string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), real, append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(bare, "init", "--bare", "-q")
	git(bare, "config", "http.receivepack", "true")
	started, release := make(chan struct{}), make(chan struct{})
	arrived := sync.OnceFunc(func() { close(started) })
	unblock := sync.OnceFunc(func() { close(release) })
	backend := &cgi.Handler{Path: real, Args: []string{"http-backend"}, Env: []string{
		"GIT_PROJECT_ROOT=" + filepath.Dir(bare), "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-receive-pack") {
			arrived()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(unblock)
	remote := "origin"
	git(root, "remote", "add", remote, server.URL+"/"+filepath.Base(bare))
	project := nodetest.Do[protocol.Project](t, owner, protocol.Command{Kind: "register_project", Data: protocol.CommandRegisterProjectPayload{Name: "Push fixture", Path: root}})
	main := nodetest.MainWorktree(t, owner, project.ID)
	inspect := func() nativeprotocol.GitStatus {
		t.Helper()
		status := nodetest.Do[nativeprotocol.GitStatus](t, owner, protocol.Command{Kind: "inspect_git", Data: nativeprotocol.CommandInspectGitPayload{Worktree: main}})
		if status.IndexRevision == nil || status.Head == nil || *status.Head != head || status.Branch == nil {
			t.Fatalf("fixture status = %+v", status)
		}
		return status
	}
	status := inspect()
	push, err := owner.Dispatch(t.Context(), owner.ID(), protocol.NewRequest(owner.ID(), protocol.Command{Kind: "run_git_action", Data: nativeprotocol.CommandRunGitActionPayload{
		Worktree: main, ExpectedIndex: *status.IndexRevision, ExpectedHead: status.Head, ExpectedBranch: status.Branch,
		Action: nativeprotocol.GitAction{Kind: "push", Data: nativeprotocol.GitActionPushPayload{Remote: &remote}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case got := <-push.Completion:
		t.Fatalf("push finished before reaching the transfer: %+v", got)
	case <-time.After(nodetest.Wait):
		t.Fatal("push did not reach the transfer")
	}

	nodetest.Do[json.RawMessage](t, owner, protocol.Command{Kind: "write_file", Data: protocol.CommandWriteFilePayload{Worktree: main, Path: "note.txt", Text: "written during the push\n"}})
	status = inspect()
	nodetest.Do[json.RawMessage](t, owner, protocol.Command{Kind: "update_git_index", Data: nativeprotocol.CommandUpdateGitIndexPayload{
		Worktree: main, Paths: []string{"note.txt"}, Operation: nativeprotocol.GitIndexChangeStage, ExpectedIndex: *status.IndexRevision, ExpectedHead: status.Head,
	}})
	if staged := git(root, "diff", "--cached", "--name-only"); staged != "note.txt" {
		t.Fatalf("local Git effect did not stage note.txt: %q", staged)
	}
	nodetest.Do[protocol.Project](t, owner, protocol.Command{Kind: "register_project", Data: protocol.CommandRegisterProjectPayload{Name: "Other fixture", Path: t.TempDir()}})
	select {
	case got := <-push.Completion:
		t.Fatalf("push finished before the transfer was released: %+v", got)
	default:
	}
	unblock()
	select {
	case got := <-push.Completion:
		if got.Fault != nil || got.Output.Kind != "git_action_completed" {
			t.Fatalf("push completion = %+v", got)
		}
	case <-time.After(nodetest.Wait):
		t.Fatal("push did not finish after release")
	}
	if pushed := git(bare, "rev-parse", "refs/heads/main"); pushed != head {
		t.Fatalf("remote branch = %s, want %s", pushed, head)
	}
}
