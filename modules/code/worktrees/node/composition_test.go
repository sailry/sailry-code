package worktreesnode_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestRepositoryModuleCombinations(t *testing.T) {
	for _, tc := range []struct {
		name           string
		git, worktrees bool
	}{
		{name: "neither"},
		{name: "git only", git: true},
		{name: "worktrees only", worktrees: true},
		{name: "both", git: true, worktrees: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var assembly native.Assembly
			if tc.git {
				assembly.Modules = append(assembly.Modules, gitnode.Module())
			}
			if tc.worktrees {
				assembly.Modules = append(assembly.Modules, worktreesnode.Module())
			}
			names := []string{"git_status", "list_worktrees", "fixture_status", "fixture_worktrees"}
			script := nodetest.NewScript(nodetest.Step{ID: "discover", Tool: "tool_search", Arguments: nodetest.Arguments(map[string]any{"query": "select:" + strings.Join(names, ","), "limit": len(names)})}, nodetest.Step{ID: "answer"})
			owner := startNode(t, assembly, script)
			root := t.TempDir()
			project := registerProject(t, owner, root)
			main := nodetest.MainWorktree(t, owner, project.ID)
			inspect := protocol.Command{Kind: "inspect_git", Data: protocol.CommandInspectGitPayload{Worktree: main}}
			list := protocol.Command{Kind: "list_worktrees", Data: protocol.CommandListWorktreesPayload{Worktree: main}}
			if tc.git {
				if result := nodetest.Command(t, owner, inspect); result.Kind != "git_status" {
					t.Fatalf("inspect_git output = %s", result.Kind)
				}
			} else {
				notAdmitted(t, owner, inspect)
				notAdmitted(t, owner, protocol.Command{Kind: "create_project", Data: nativeprotocol.ProjectsDraft{
					Name: "Unavailable clone", Path: filepath.Join(root, "clone"), Appearance: protocol.DefaultProjectsAppearance(),
					Source: nativeprotocol.ProjectsSource{Kind: "clone", Data: nativeprotocol.ProjectsSourceClonePayload{Url: root}},
				}})
			}
			if tc.worktrees {
				if result := nodetest.Command(t, owner, list); result.Kind != "git_worktrees" {
					t.Fatalf("list_worktrees output = %s", result.Kind)
				}
			} else {
				notAdmitted(t, owner, list)
				notAdmitted(t, owner, nodetest.DecodeCommand(t, "register_worktree", map[string]any{"project": project.ID, "path": root}))
			}
			stream, err := owner.Subscribe(t.Context(), owner.ID(), protocol.Topic{Kind: "repository", Data: main})
			if stream != nil {
				defer stream.Close()
			}
			if tc.git {
				if err != nil || stream == nil {
					t.Fatalf("repository subscription = %v, %v", stream, err)
				}
			} else if stream != nil || !nodetest.FaultIs(err, protocol.ErrorCodeNotConfigured) {
				t.Fatalf("repository subscription exposed by another capability: %v, %v", stream, err)
			}

			nodetest.Command(t, owner, protocol.Command{Kind: "write_file", Data: protocol.CommandWriteFilePayload{Worktree: main, Path: "ordinary.txt", Text: "shared workspace\n"}})
			body := nodetest.Do[protocol.FileContent](t, owner, protocol.Command{Kind: "read_file", Data: protocol.CommandReadFilePayload{Worktree: main, Path: "ordinary.txt"}})
			if body.Text != "shared workspace\n" {
				t.Fatalf("shared file read = %+v", body)
			}
			const name = "fixture-workspace"
			info := nodetest.InstallPlugin(t, owner, name, map[string]string{"plugin.json": `{"$schema":"` + sdktest.ManifestSchema + `","name":"` + name + `","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","scope":"host","actions":["git.read","worktrees.read"],"tools":[{"name":"fixture_status","operation":"git.status"},{"name":"fixture_worktrees","operation":"worktrees.list"}]}}}`})
			session := nodetest.ProjectSession(t, owner, project.ID, protocol.PermissionAsk)
			if session.Worktree != main {
				t.Fatal("project session lost its workspace identity")
			}
			frozen, results := nodetest.Turn(t, owner, session, "Discover repository tools for this project", nodetest.Wait, nil)
			if !slices.Contains(frozen, sdktest.PackageReference(info)) {
				t.Fatalf("installed fixture was not captured: %v", frozen)
			}
			found, ok := nodetest.Result(t, results, "discover", "tool_search")["tools"].([]any)
			if !ok {
				t.Fatalf("unexpected discovery result: %v", found)
			}
			_, offered := script.Observation("")
			for name, want := range map[string]bool{
				"git_status": tc.git, "fixture_status": tc.git,
				"list_worktrees": tc.worktrees, "fixture_worktrees": tc.worktrees,
			} {
				matches := func(tool string) bool { return nodetest.DeclaredToolName(tool) == name }
				discovered := slices.ContainsFunc(found, func(value any) bool {
					description, _ := value.(map[string]any)
					tool, _ := description["name"].(string)
					return matches(tool)
				})
				if visible := discovered || slices.ContainsFunc(offered, matches); visible != want {
					t.Errorf("tool %q visible=%t, want %t; discovered %v, offered %v", name, visible, want, found, offered)
				}
			}
		})
	}
}

func TestWorktreesOnlyLifecycleAndReceipt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git executable is required")
	}
	script := nodetest.NewScript()
	owner := startNode(t, native.Assembly{Modules: []native.Module{worktreesnode.Module()}}, script)
	root, head := nodetest.GitProject(t, map[string]string{"source.txt": "contents\n"})
	project := registerProject(t, owner, root)
	main := nodetest.MainWorktree(t, owner, project.ID)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "linked")
	request := protocol.NewRequest(owner.ID(), nodetest.DecodeCommand(t, "create_worktree", map[string]any{"project": project.ID, "path": path, "branch": "feature", "commit": head}))
	created, failure := nodetest.Request[protocol.Worktree](t, owner, request, nodetest.Wait)
	if failure != nil || created.Path != path || created.Project == nil || *created.Project != project.ID || created.Main {
		t.Fatalf("create_worktree = %+v, %v", created, failure)
	}
	replayed, failure := nodetest.Request[protocol.Worktree](t, owner, request, nodetest.Wait)
	if failure != nil || replayed.ID != created.ID || replayed.Path != created.Path {
		t.Fatalf("create replay = %+v, %v (first %+v)", replayed, failure, created)
	}
	list := nodetest.Do[nativeprotocol.GitWorktrees](t, owner, protocol.Command{Kind: "list_worktrees", Data: protocol.CommandListWorktreesPayload{Worktree: main}})
	if len(list.Entries) != 2 || list.Entries[0].Path != root || list.Entries[1].Path != path {
		t.Fatalf("unified worktree listing = %+v", list)
	}
	script.Reset(
		nodetest.Step{ID: "discover", Tool: "tool_search", Arguments: nodetest.Arguments(map[string]any{"query": "select:register_worktree"})},
		nodetest.Step{ID: "register", Tool: "register_worktree", Arguments: nodetest.Arguments(map[string]string{"path": created.Path})},
		nodetest.Step{ID: "answer"},
	)
	session := nodetest.ProjectSession(t, owner, project.ID, protocol.PermissionAsk)
	approved := 0
	_, results := nodetest.Turn(t, owner, session, "Register the linked checkout", nodetest.Wait, func(approval protocol.ConversationApproval) {
		approved++
		if approval.Session != session.ID {
			t.Fatalf("worktree tool used another session's approval: %+v", approval)
		}
	})
	result := nodetest.Result(t, results, "register", "register_worktree")
	data, _ := result["data"].(map[string]any)
	if approved != 1 || result["kind"] != "worktree" || data["id"] != created.ID.String() {
		t.Fatalf("approved worktree registration returned %v with %d approvals", result, approved)
	}
	notAdmitted(t, owner, protocol.Command{Kind: "inspect_git", Data: protocol.CommandInspectGitPayload{Worktree: created.ID}})
	remove := func(id protocol.WorktreeID, branch string) protocol.Command {
		return protocol.Command{Kind: "remove_worktree", Data: protocol.CommandRemoveWorktreePayload{Worktree: id, ExpectedHead: head, ExpectedBranch: branch}}
	}
	if _, fault := nodetest.Call[json.RawMessage](t, owner, remove(created.ID, "stale")); fault == nil || fault.Code != protocol.ErrorCodeRevisionConflict {
		t.Fatalf("stale branch removal = %v", fault)
	}
	if _, err := os.Stat(filepath.Join(created.Path, "source.txt")); err != nil {
		t.Fatalf("stale removal changed the checkout: %v", err)
	}
	assertRemoved := func(tree protocol.Worktree) {
		t.Helper()
		if _, err := os.Stat(tree.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("removed checkout remains on disk: %v", err)
		}
		if registeredWorktree(t, owner, tree.ID) {
			t.Fatal("removed checkout remains registered")
		}
	}
	nodetest.Command(t, owner, remove(created.ID, "feature"))
	assertRemoved(created)

	adopt := func(root string) protocol.Command {
		return protocol.Command{Kind: "register_worktree", Data: nativeprotocol.CommandRegisterWorktreePayload{Project: project.ID, Path: root}}
	}
	if current := nodetest.Do[protocol.Worktree](t, owner, adopt(root)); current.ID != main || !current.Main {
		t.Fatalf("adopting the main checkout changed its registration: %+v", current)
	}
	foreign, _ := nodetest.GitProject(t, map[string]string{"other.txt": "another repository\n"})
	if _, failure := nodetest.Call[protocol.Worktree](t, owner, adopt(foreign)); failure == nil || failure.Code != nativeprotocol.ErrorCodeConflict {
		t.Fatalf("adoption of an unrelated repository = %v", failure)
	}
	external := filepath.Join(parent, "outside")
	worktreeGit(t, root, "worktree", "add", "-b", "outside", external, head)
	adopted := nodetest.Do[protocol.Worktree](t, owner, adopt(external))
	if adopted.Path != external || adopted.Project == nil || *adopted.Project != project.ID || adopted.Main || !registeredWorktree(t, owner, adopted.ID) {
		t.Fatalf("adopted checkout was not registered: %+v", adopted)
	}
	nodetest.Command(t, owner, remove(adopted.ID, "outside"))
	assertRemoved(adopted)
}

func registeredWorktree(t *testing.T, owner *native.Node, id protocol.WorktreeID) bool {
	t.Helper()
	return slices.ContainsFunc(nodetest.Snapshot(t, owner).Worktrees, func(tree protocol.Worktree) bool { return tree.ID == id })
}

func worktreeGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func startNode(t *testing.T, assembly native.Assembly, script *nodetest.Script) *native.Node {
	t.Helper()
	options := native.Options{Assembly: assembly}
	var owner *native.Node
	if script == nil {
		owner = nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), nil, options)
	} else {
		owner = nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), script, options)
	}
	t.Cleanup(func() {
		if err := owner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return owner
}

func registerProject(t *testing.T, owner *native.Node, root string) protocol.Project {
	t.Helper()
	return nodetest.Do[protocol.Project](t, owner, protocol.Command{Kind: "register_project", Data: protocol.CommandRegisterProjectPayload{Name: "Repository fixture", Path: root}})
}

func notAdmitted(t *testing.T, owner *native.Node, command protocol.Command) {
	t.Helper()
	accepted, err := owner.Dispatch(t.Context(), owner.ID(), protocol.NewRequest(owner.ID(), command))
	if !nodetest.FaultIs(err, protocol.ErrorCodeNotConfigured) || accepted.Completion != nil {
		t.Fatalf("%s was not rejected before admission: receipt=%+v, error=%v", command.Kind, accepted.Receipt, err)
	}
}
