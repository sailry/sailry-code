package worktreesdesktop

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestManagerEntries(t *testing.T) {
	head := new("0123456789abcdef0123456789abcdef01234567")
	trees := protocol.GitWorktrees{Entries: []protocol.GitWorktree{
		{Path: "/repo", Branch: new("main"), Head: head, Main: true, Available: true},
		{Path: "/trees/feature", Branch: new("feature"), Head: head, Available: true},
		{Path: "/trees/current", Branch: new("current"), Head: head, Available: true},
		{Path: "/trees/locked", Branch: new("locked"), Head: head, Locked: true, Available: true},
		{Path: `C:\trees\detached\`, Head: head, Available: true},
		{Path: "/trees/gone", Branch: new("gone"), Head: head},
		{Path: "/trees/gone-locked", Branch: new("kept"), Head: head, Locked: true},
	}}
	entries := worktreeEntries(trees, "/trees/current")
	type row struct {
		label                        string
		marks                        []string
		current, stale, open, remove bool
	}
	want := []row{
		{label: "main", marks: []string{"worktree_main"}, open: true},
		{label: "feature", open: true, remove: true},
		{label: "current", current: true},
		{label: "locked", marks: []string{"worktree_locked"}, open: true},
		{label: "01234567", open: true},
		{label: "gone", marks: []string{"worktree_prune_on_select"}, stale: true},
		{label: "kept", marks: []string{"worktree_locked", "worktree_unavailable"}},
	}
	if len(entries) != len(want) {
		t.Fatalf("manager lists %d of %d worktrees", len(entries), len(want))
	}
	for index, entry := range entries {
		got := row{entry.label, entry.marks, entry.current, entry.stale, entry.open, entry.remove}
		if !reflect.DeepEqual(got, want[index]) {
			t.Errorf("%s: %+v, want %+v", entry.tree.Path, got, want[index])
		}
	}
	for path, name := range map[string]string{"/trees/feature": "feature", `C:\trees\detached\`: "detached", "/": "/", "plain": "plain"} {
		if got := worktreeName(path); got != name {
			t.Errorf("worktreeName(%q) = %q, want %q", path, got, name)
		}
	}
	if label := worktreeLabel(protocol.GitWorktree{Path: "/trees/unborn"}); label != "unborn" {
		t.Errorf("a worktree without a branch or commit is labelled %q", label)
	}
}

func TestManagerWorktreesNotUsedBySessions(t *testing.T) {
	trees := []protocol.Worktree{
		{ID: protocol.WorktreeID{1}, Path: "/repo", Main: true},
		{ID: protocol.WorktreeID{2}, Path: "/trees/used"},
		{ID: protocol.WorktreeID{3}, Path: "/trees/left"},
	}
	sessions := []protocol.Session{{Worktree: protocol.WorktreeID{2}}}
	if got := unusedWorktrees(trees, sessions); len(got) != 1 || !got["/trees/left"] {
		t.Fatalf("unused worktrees = %v", got)
	}
}

func TestManagerRowsAndRegistration(t *testing.T) {
	project := protocol.ProjectID{1}
	main := protocol.Worktree{ID: protocol.WorktreeID{1}, Project: &project, Path: "/fixture", Main: true}
	linked := protocol.Worktree{ID: protocol.WorktreeID{2}, Project: &project, Path: "/trees/feature"}
	head := new("0123456789abcdef0123456789abcdef01234567")
	trees := protocol.GitWorktrees{Entries: []protocol.GitWorktree{
		{Path: main.Path, Branch: new("main"), Head: head, Main: true, Available: true},
		{Path: linked.Path, Branch: new("feature"), Head: head, Available: true},
		{Path: "/trees/gone", Branch: new("gone"), Head: head},
	}}
	managerFrame(t, func(c *ui.Context) {
		var rows []native.WorkspacePickerRow
		var keys []string
		var sent []string
		var admitted []protocol.Worktree
		refused, current, canSelect := true, true, true
		selected := main
		workspace := native.WorkflowWorkspace{
			Worktree: main, Current: func() bool { return current },
			CanSelectLocation: func() bool { return canSelect },
			Pick:              func(_ string, result []native.WorkspacePickerRow) { rows = result },
			Locations:         func() ([]protocol.Worktree, []protocol.Session) { return admitted, nil },
			Admit:             func(tree protocol.Worktree) { admitted = append(admitted, tree) },
			SelectLocation:    func(_ *ui.Context, tree protocol.Worktree) { selected = tree },
			Report:            func(_ *ui.Context, _ error, key, _, _ string) { keys = append(keys, key) },
			Execute: func(_ string, command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
				sent = append(sent, command.Kind)
				switch command.Kind {
				case "list_worktrees":
					done(c, protocol.Output{}, errors.New("offline"))
				case "register_worktree":
					if input := command.Data.(protocol.CommandRegisterWorktreePayload); input.Project != project || input.Path != linked.Path {
						t.Fatalf("registration: %+v", input)
					}
					if refused {
						done(c, protocol.Output{}, protocol.Fault{Code: protocol.ErrorCodeConflict, Message: "worktree is registered to another project"})
					} else {
						done(c, protocol.Output{Kind: "worktree", Data: linked}, nil)
					}
				default:
					t.Fatalf("unexpected request %q", command.Kind)
				}
			},
		}
		rows = worktreeRows(c, workspace, trees)
		type shown struct {
			label, detail, group string
			checked, chosen      bool
		}
		want := []shown{
			{"main", "Main · fixture", "Worktrees", true, true},
			{"feature", "feature", "Worktrees", false, true},
			{"gone", "Unavailable · click to clean up · gone", "Worktrees", false, true},
			{"Create and use worktree", "", "Worktree actions", false, true},
		}
		if len(rows) != len(want) {
			t.Fatalf("manager shows %d rows, want %d", len(rows), len(want))
		}
		for index, row := range rows {
			if got := (shown{row.Label, row.Detail, row.Group, row.Checked, row.Run != nil && !row.Disabled}); got != want[index] {
				t.Errorf("row %d: %+v, want %+v", index, got, want[index])
			}
		}
		entries := worktreeEntries(trees, main.Path)
		if got := worktreeActions(c, workspace, entries[0]); len(got) != 1 || got[0].Label != "Copy path" {
			t.Fatalf("current main worktree actions: %+v", got)
		}
		actions := worktreeActions(c, workspace, entries[1])
		if len(actions) != 3 || actions[0].Label != "Open worktree" || actions[1].Label != "Copy path" || actions[2].Label != "Remove worktree" {
			t.Fatalf("linked worktree actions: %+v", actions)
		}
		canSelect = false
		actions[0].Run(c)
		if len(sent) != 0 || selected.ID != main.ID || !slices.Equal(keys, []string{"worktree_open_busy"}) {
			t.Fatalf("busy location: selected=%v, requests=%q, reports=%q", selected.ID, sent, keys)
		}
		canSelect = true
		actions[0].Run(c)
		if selected.ID != main.ID || !slices.Contains(keys, "worktree_conflict") {
			t.Fatalf("refused registration: selected=%v, reports=%q", selected.ID, keys)
		}
		refused = false
		actions[0].Run(c)
		actions[0].Run(c)
		if selected.ID != linked.ID || len(admitted) != 1 || !slices.Equal(sent, []string{"register_worktree", "register_worktree"}) {
			t.Fatalf("selected=%v, admitted=%v, requests=%q", selected.ID, admitted, sent)
		}
		current = false
		actions[0].Run(c)
		if !slices.Contains(keys, "worktree_context_changed") {
			t.Fatalf("changed location reports %q", keys)
		}
		current = true
		Manage(workspace)
		if !slices.Contains(keys, "worktree_load_failed") {
			t.Fatalf("failed listing reports %q", keys)
		}
	})
}

func TestRemovalChecksDraftsAndConfirmsExpectedCheckout(t *testing.T) {
	managerFrame(t, func(c *ui.Context) {
		project := protocol.ProjectID{1}
		main := protocol.Worktree{ID: protocol.WorktreeID{1}, Project: &project, Path: "/repo", Main: true}
		linked := protocol.Worktree{ID: protocol.WorktreeID{2}, Project: &project, Path: "/trees/linked"}
		entry := protocol.GitWorktree{Path: linked.Path, Branch: new("linked"), Head: new("head"), Available: true}
		draft := true
		var submit func(func(bool))
		var requests []string
		var key string
		var removed protocol.WorktreeID
		workspace := native.WorkflowWorkspace{
			Worktree: main,
			Current:  func() bool { return true },
			Confirm: func(title, target string, action func(func(bool)), consequence ...string) {
				if title != "worktree_remove" || target != linked.Path || !slices.Equal(consequence, []string{"worktree_remove_confirm"}) {
					t.Fatalf("confirmation %q for %q: %v", title, target, consequence)
				}
				submit = action
			},
			Locations:          func() ([]protocol.Worktree, []protocol.Session) { return []protocol.Worktree{linked}, nil },
			HasDrafts:          func(id protocol.WorktreeID) bool { return id == linked.ID && draft },
			Release:            func(id protocol.WorktreeID) { removed = id },
			SetPromptUncertain: func(bool) {},
			Refresh:            func() {},
			Report:             func(_ *ui.Context, _ error, failed, _, _ string) { key = failed },
			Execute: func(_ string, command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
				requests = append(requests, command.Kind)
				input, ok := command.Data.(protocol.CommandRemoveWorktreePayload)
				if command.Kind != "remove_worktree" || !ok || input.Worktree != linked.ID || input.ExpectedHead != "head" || input.ExpectedBranch != "linked" {
					t.Fatalf("unexpected removal: %+v", command)
				}
				done(c, protocol.Output{Kind: "worktree_removed", Data: protocol.OutputWorktreeRemovedPayload{ID: linked.ID}}, nil)
			},
		}
		confirmRemoval(c, workspace, entry)
		if submit == nil || len(requests) != 0 {
			t.Fatal("the manager did not wait for confirmation")
		}
		submit(func(success bool) {
			if success || len(requests) != 0 || key != "worktree_remove_unsaved" {
				t.Fatalf("unsaved removal: success=%v, requests=%q, report=%q", success, requests, key)
			}
		})
		draft = false
		submit(func(success bool) {
			if !success || len(requests) != 1 || removed != linked.ID {
				t.Fatalf("confirmed removal: success=%v, requests=%q, removed=%v", success, requests, removed)
			}
		})
	})
}

func TestManagerCreateAndUseRechecksLocationAfterCreation(t *testing.T) {
	for _, test := range []struct {
		name, warning string
		current, idle bool
	}{
		{name: "same idle location", current: true, idle: true},
		{name: "session became busy", current: true, warning: "worktree_open_busy"},
		{name: "workspace changed", idle: true, warning: "worktree_context_changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			managerFrame(t, func(c *ui.Context) {
				project := protocol.ProjectID{1}
				source := protocol.Worktree{ID: protocol.WorktreeID{1}, Project: &project, Path: "/fixture"}
				created := protocol.Worktree{ID: protocol.WorktreeID{2}, Project: &project, Path: "/linked"}
				current, idle := true, true
				var reported, commands []string
				var admitted, selected protocol.WorktreeID
				var submit func([]string, func(bool))
				var settle func(*ui.Context, protocol.Output, error)
				workspace := native.WorkflowWorkspace{
					Worktree: source, Current: func() bool { return current }, CanSelectLocation: func() bool { return idle },
					Report: func(_ *ui.Context, _ error, key, _, _ string) { reported = append(reported, key) },
					Prompt: func(title string, _, _ []string, action func([]string, func(bool)), _ ...native.WorkspacePromptToggle) {
						if title != "worktree_create_and_use" {
							t.Fatalf("opened the wrong form: %q", title)
						}
						submit = action
					},
					SetPromptAction: func(key string) {
						if key != "worktree_create_and_use" {
							t.Fatalf("form action = %q", key)
						}
					},
					SetPromptUncertain: func(bool) {},
					Admit:              func(tree protocol.Worktree) { admitted = tree.ID },
					SelectLocation:     func(_ *ui.Context, tree protocol.Worktree) { selected = tree.ID },
					Refresh:            func() {},
					Execute: func(_ string, command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
						commands = append(commands, command.Kind)
						switch command.Kind {
						case "resolve_git_revision":
							done(c, protocol.Output{Kind: "git_revision", Data: protocol.OutputGitRevisionPayload{Commit: "commit"}}, nil)
						case "create_worktree":
							settle = done
						default:
							t.Fatalf("unexpected command %q", command.Kind)
						}
					},
				}
				createAt(c, workspace, "main", "commit", true)
				if submit == nil {
					t.Fatal("the manager did not show its create-and-use form")
				}
				var completed []bool
				submit([]string{"main", "feature", created.Path}, func(ok bool) { completed = append(completed, ok) })
				if settle == nil || len(completed) != 0 || selected != (protocol.WorktreeID{}) {
					t.Fatal("the location changed before the Node confirmed creation")
				}
				current, idle = test.current, test.idle
				settle(c, protocol.Output{Kind: "worktree", Data: created}, nil)
				if admitted != created.ID || !slices.Equal(completed, []bool{true}) || !slices.Equal(commands, []string{"resolve_git_revision", "create_worktree"}) {
					t.Fatalf("creation was not confirmed exactly once: admitted=%s, completed=%v, commands=%q", admitted, completed, commands)
				}
				if test.warning == "" {
					if selected != created.ID || len(reported) != 0 {
						t.Fatalf("new checkout was not selected: %s; reports %q", selected, reported)
					}
				} else if selected != (protocol.WorktreeID{}) || !slices.Equal(reported, []string{test.warning}) {
					t.Fatalf("a retired or busy location was changed: %s; reports %q", selected, reported)
				}
			})
		})
	}
}

func TestManagerFailureCopy(t *testing.T) {
	fault := func(code protocol.ErrorCode, message string) error {
		return protocol.Fault{Code: code, Message: message}
	}
	tests := []struct {
		action string
		err    error
		key    string
	}{
		{"create_worktree", fault(protocol.ErrorCodeConflict, "branch already exists"), "git_branch_exists"},
		{"create_worktree", fault(protocol.ErrorCodeConflict, "destination already exists"), "worktree_path_exists"},
		{"create_worktree", fault(protocol.ErrorCodeConflict, "worktree directory is already registered"), "worktree_conflict"},
		{"create_worktree", fault(protocol.ErrorCodeInvalidRequest, "expected an absolute new directory"), "worktree_invalid"},
		{"create_worktree", fault(protocol.ErrorCodeNotFound, ""), "worktree_not_found"},
		{"create_worktree", fault(protocol.ErrorCodeOutcomeUnknown, ""), "worktree_outcome_unknown"},
		{"register_worktree", fault(protocol.ErrorCodeConflict, "worktree is registered to another project"), "worktree_conflict"},
		{"register_worktree", errors.New("offline"), "worktree_request_failed"},
		{"remove_worktree", fault(protocol.ErrorCodeBusy, "worktree is locked"), "worktree_remove_locked"},
		{"remove_worktree", fault(protocol.ErrorCodeBusy, "worktree has active commands"), "worktree_remove_busy"},
		{"remove_worktree", fault(protocol.ErrorCodeConflict, "worktree has uncommitted changes"), "worktree_remove_dirty"},
		{"remove_worktree", fault(protocol.ErrorCodeRevisionConflict, ""), "worktree_remove_conflict"},
		{"remove_worktree", fault(protocol.ErrorCodeNotFound, ""), "worktree_remove_missing"},
		{"remove_worktree", fault(protocol.ErrorCodeOutcomeUnknown, ""), "worktree_outcome_unknown"},
		{"prune_worktree", fault(protocol.ErrorCodeBusy, "worktree is locked"), "worktree_remove_locked"},
		{"prune_worktree", fault(protocol.ErrorCodeBusy, "Git index is locked"), "git_index_busy"},
		{"prune_worktree", fault(protocol.ErrorCodeConflict, "worktree path still exists"), "worktree_prune_exists"},
		{"prune_worktree", fault(protocol.ErrorCodeRevisionConflict, ""), "git_index_conflict"},
		{"prune_worktree", fault(protocol.ErrorCodeInvalidRequest, ""), "worktree_invalid"},
		{"prune_worktree", fault(protocol.ErrorCodeUnavailable, ""), "worktree_prune_failed"},
	}
	keys := []string{"worktree_main", "worktree_locked", "worktree_unavailable", "worktree_prune_on_select", "worktree_listing_partial",
		"worktree_base", "worktree_path", "worktree_actions", "workspace_open_worktree", "worktree_load_failed", "worktree_missing_project",
		"worktree_no_repository", "worktree_revision_failed", "worktree_context_changed", "worktree_open_busy", "worktree_created",
		"worktree_removed", "worktree_remove_confirm", "worktree_remove_unsaved", "worktree_prune", "worktree_prune_confirm", "worktree_pruned", "worktree_unused"}
	for _, test := range tests {
		if got := worktreeFailure(test.action, test.err); got != test.key {
			t.Errorf("%s %v: %q, want %q", test.action, test.err, got, test.key)
		}
		keys = append(keys, test.key)
	}
	for _, lang := range []string{"en", "zh-CN"} {
		for _, key := range keys {
			if i18n.In(lang, key) == key {
				t.Errorf("%s has no copy for %q", lang, key)
			}
		}
	}
}

func managerFrame(t *testing.T, run func(*ui.Context)) {
	t.Helper()
	catalog := i18n.New(locales)
	tester := ui.NewTester(func(c *ui.Context) { i18n.Bind(c, catalog); run(c) }, 640, 400)
	tester.SetLocale("en")
	tester.Frame()
}
