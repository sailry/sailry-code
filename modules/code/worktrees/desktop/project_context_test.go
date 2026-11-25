package worktreesdesktop_test

import (
	"os"
	"os/exec"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

func TestProjectWorktreeContextAndCreate(t *testing.T) {
	assembly := native.Assemble(git.Register, worktrees.Register)
	if len(assembly.ProjectWorkflows) != 1 || len(assembly.ExecutionSettings) != 0 {
		t.Fatal("the worktree module must contribute its workflow without adding execution settings")
	}
	var asked []string
	bind := assembly.ProjectWorkflows[0]
	assembly.ProjectWorkflows[0] = func(host native.ProjectWorkflowHost) {
		host.NewDraft = func(project *native.ProjectMenuProject) (native.WorkflowConversation, bool) {
			asked = append(asked, project.Name)
			return native.WorkflowConversation{}, false
		}
		bind(host)
	}
	w := support.Open(t, node.Assembly{Modules: []node.Module{gitnode.Module(), worktreesnode.Module()}}, assembly)
	plain, repository := t.TempDir(), t.TempDir()
	init := exec.Command("git", "-C", repository, "init", "-b", "main")
	init.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	w.CreateProject("Plain folder", plain)
	project := w.CreateProject("Git folder", repository)
	w.App.Navigate(native.ConversationPage)
	w.App.Sidebar.NewChat()
	controls := func() (bar []ui.AccessInfo) {
		for _, info := range w.Tester.AccessAll() {
			if b := info.Bounds; info.Role == ui.RoleMenuButton && b.H == 24 && b.W > 0 && b.Y > native.HeaderHeight {
				bar = append(bar, info)
			}
		}
		return
	}
	names := func() (result []string) {
		for _, info := range controls() {
			result = append(result, info.Name)
		}
		return
	}
	w.Await("the initial chat has more than its project selector", func() bool {
		return slices.Equal(names(), []string{support.Label("composer_no_project")}) && len(w.App.Data.Projects) == 2
	})
	choose := func(project string) {
		t.Helper()
		w.Click(controls()[0])
		var item ui.AccessInfo
		w.Await("the location menu does not offer "+project, func() bool {
			for _, role := range []ui.Role{ui.RoleMenuItem, ui.RoleMenuItemCheckBox, ui.RoleMenuItemRadio} {
				if found, ok := w.Tester.AccessByRole(role, project); ok {
					item = found
					return true
				}
			}
			return false
		})
		w.Click(item)
	}
	choose("Plain folder")
	w.Await("a plain directory offers a branch", func() bool { return slices.Equal(names(), []string{"Plain folder"}) })
	choose("Git folder")
	w.Await("the Git context lacks its project or main branch", func() bool {
		return slices.Equal(names(), []string{"Git folder", "main"})
	})
	bar := controls()
	if bar[0].Bounds.Y != bar[1].Bounds.Y || bar[1].Bounds.X < bar[0].Bounds.X+bar[0].Bounds.W {
		t.Errorf("the project %v and the branch %v are not side by side on one line", bar[0].Bounds, bar[1].Bounds)
	}
	w.ProjectMenu(project.Name)
	if err := w.Tester.ChooseMenuItem(support.Label("worktree_manage"), support.Label("worktree_create")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []string{project.Name}) {
		t.Fatalf("the create worktree entry asked for drafts in %q", asked)
	}
}
