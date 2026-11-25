package desktop_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	desktop "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitdesktop "github.com/sailry/sailry-code/modules/code/git/desktop"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktreesdesktop "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	node "github.com/sailry/sailry-harness/sdk/native/node"
	wire "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestGitInspectionFailuresWithoutFiles(t *testing.T) {
	const detail = "fixture Git status process failed"
	unavailable := errors.New(detail)
	denied := &wire.Fault{Code: protocol.ErrorCodePermissionDenied, Message: "fixture Git permission denied"}
	for _, tc := range []struct {
		name, toast string
		cause       error
		code        protocol.ErrorCode
		linked      bool
	}{
		{"missing executable", "git_inspection_failed", &wire.Fault{Code: protocol.ErrorCodeNotConfigured, Message: "Git executable not found on execution Node"}, protocol.ErrorCodeNotConfigured, false},
		{"failed status process", "git_inspection_failed", unavailable, protocol.ErrorCodeUnavailable, false},
		{"permission denied", "git_index_denied", denied, protocol.ErrorCodePermissionDenied, false},
		{"linked status and project location share a failure", "git_inspection_failed", unavailable, protocol.ErrorCodeUnavailable, true},
		{"linked permission failure differs from project location failure", "git_index_denied", denied, protocol.ErrorCodePermissionDenied, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, _ := nodetest.GitProject(t, map[string]string{"notes.txt": "workspace fixture\n"})
			selected := main
			host := node.Assembly{Modules: []node.Module{gitnode.Module()}}
			modules := []native.Module{gitdesktop.Register}
			if tc.linked {
				selected = linkedInspectionCheckout(t, main)
				host.Modules = append(host.Modules, worktreesnode.Module())
				modules = append(modules, worktreesdesktop.Register)
			}
			profile := filepath.Join(t.TempDir(), "profile")
			closeLog, err := sdktest.InstallLogging(profile, "desktop")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(closeLog)
			owner := nodetest.Start(t, profile, nil, node.Options{Assembly: host, Git: sdktest.GitOptions{Run: func(_ context.Context, root string, args []string, _ []byte) (sdktest.GitCommandResult, error) {
				switch {
				case slices.Contains(args, "status") && root == selected:
					return sdktest.GitCommandResult{}, tc.cause
				case tc.linked && root == main && slices.Contains(args, "worktree") && slices.Contains(args, "list"):
					return sdktest.GitCommandResult{}, unavailable
				default:
					t.Errorf("unexpected native Git read: root=%q args=%v", root, args)
					return sdktest.GitCommandResult{}, errors.New("unexpected native Git read")
				}
			}}})
			t.Cleanup(func() { _ = owner.Stop(context.Background()) })
			project := nodetest.Do[protocol.Project](t, owner, protocol.Command{Kind: "register_project", Data: protocol.CommandRegisterProjectPayload{Name: "Git inspection fixture", Path: main}})
			mainID := nodetest.MainWorktree(t, owner, project.ID)
			selectedID := mainID
			if tc.linked {
				tree := nodetest.Do[protocol.Worktree](t, owner, protocol.Command{Kind: "register_worktree", Data: wire.CommandRegisterWorktreePayload{Project: project.ID, Path: selected}})
				selectedID = tree.ID
			}
			config := nodetest.SessionConfig(t, owner, 32000, protocol.PermissionProject)
			session := nodetest.Do[protocol.Session](t, owner, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project.ID, Worktree: &selectedID, Config: &config}})
			w := (&desktop.Node{T: t, Ctx: t.Context(), Profile: profile, Owner: owner}).Connect(native.Assemble(modules...))
			var listings atomic.Int32
			w.Controller.OnDispatch(func(kind string) {
				if kind == "list_directory" {
					listings.Add(1)
				}
			})
			w.OpenSession(session.ID.String())
			label := desktop.Label(tc.toast)
			locationsLabel := desktop.Label("git_inspection_failed")
			distinct := tc.linked && locationsLabel != label
			w.Await("the selected repository did not report the Git failure", func() bool {
				watch, watching := w.Controller.Workspace(selectedID, true)
				return watching && watch.Connected && w.App.FocusedConversation() == session.ID.String() &&
					w.Tester.HasText(label) && (!tc.linked || len(inspectionLogRecords(t, "desktop", "list_worktrees")) > 0) &&
					(!distinct || w.Tester.HasText(locationsLabel))
			})
			assertToasts := func() {
				t.Helper()
				texts := w.Tester.Texts()
				if countText(texts, label) != 1 || distinct && countText(texts, locationsLabel) != 1 || w.Tester.HasText(desktop.Label("workspace_action_failed")) {
					t.Fatalf("Git failed to report each distinct cause once: %q", texts)
				}
			}
			assertToasts()
			if tc.linked {
				if !slices.ContainsFunc(inspectionLogRecords(t, "command", "list_worktrees"), func(line string) bool {
					return strings.Contains(line+" ", " worktree="+mainID.String()+" ")
				}) {
					t.Fatal("project location did not read the main worktree")
				}
			}
			if tc.linked && !distinct {
				w.Tester.Advance(time.Minute)
				w.Await("the original toast did not retire", func() bool { return !w.Tester.HasText(label) })
				before := len(inspectionLogRecords(t, "desktop", "inspect_git"))
				observed, _ := w.Controller.Workspace(selectedID, true)
				if err := os.WriteFile(filepath.Join(selected, "watch-pulse.txt"), []byte("changed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				w.Await("a repository change did not repeat the inspection", func() bool {
					watch, watching := w.Controller.Workspace(selectedID, true)
					return watching && watch.Revision > observed.Revision && len(inspectionLogRecords(t, "desktop", "inspect_git")) > before
				})
				if w.Tester.HasText(label) {
					t.Fatal("the same watch failure repeated an already delivered toast")
				}
				workspace := w.App.Hooks.WorkspaceProject()
				if workspace == nil || workspace.ID != selectedID.String() {
					t.Fatal("the window did not retain its linked workspace")
				}
				before = len(inspectionLogRecords(t, "desktop", "inspect_git"))
				workspace.Actions.Refresh()
				w.Await("explicit refresh did not report the persisting failure", func() bool {
					return w.Tester.HasText(label) && len(inspectionLogRecords(t, "desktop", "inspect_git")) > before
				})
				assertToasts()
			}
			if listings.Load() != 0 || w.App.NativePageEnabled(native.FilesPage) || len(w.App.Panel.Tabs) != 0 {
				t.Fatal("repository failure mounted or read the Files tree, or opened a panel tab")
			}
			refs := map[string]bool{}
			for _, line := range inspectionLogRecords(t, "command", "inspect_git") {
				if strings.Contains(line+" ", " worktree="+selectedID.String()+" ") && strings.Contains(line+" ", " code="+string(tc.code)+" ") &&
					strings.Contains(line, " stage=run_status ") && strings.Contains(line, " reason=status_command_failed ") {
					refs[inspectionReportRef(line)] = true
				}
			}
			shown := inspectionLogRecords(t, "desktop", "inspect_git")
			if len(shown) == 0 {
				t.Fatal("the specific toast had no Desktop diagnostic")
			}
			for _, line := range shown {
				ref := inspectionReportRef(line)
				if !sdktest.ValidReportRef(ref) || !refs[ref] || !strings.Contains(line, " WARN desktop ") ||
					!strings.Contains(line, " stage=run_status ") || !strings.Contains(line, " reason=status_command_failed ") || !strings.Contains(line, " detail=") ||
					tc.cause == unavailable && !strings.Contains(line, detail) {
					t.Fatalf("Desktop diagnostic did not uniquely correlate to a scoped Node report: %s", line)
				}
				delete(refs, ref)
			}
		})
	}
}

func linkedInspectionCheckout(t *testing.T, root string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("linked checkout fixture requires Git")
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "linked")
	command := exec.CommandContext(t.Context(), git, "-C", root, "worktree", "add", "-b", "linked", path)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create linked checkout: %v: %s", err, output)
	}
	return path
}

func inspectionLogRecords(t *testing.T, area, operation string) []string {
	t.Helper()
	path, err := sdktest.CurrentLogFile()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	return slices.DeleteFunc(lines, func(line string) bool {
		return !strings.Contains(line, " area="+area+" phase=failed ") || !strings.Contains(line+" ", " operation="+operation+" ")
	})
}

func inspectionReportRef(line string) string {
	_, raw, _ := strings.Cut(line, " report_ref=")
	ref, _, _ := strings.Cut(raw, " ")
	return ref
}
