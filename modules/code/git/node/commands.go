package gitnode

import (
	"context"
	"time"

	git "github.com/sailry/sailry-code/modules/code/git/service"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

const repositoryReadTimeout = 5 * time.Second
const remoteReadTimeout = 125 * time.Second

func bindWorkspace(repo *git.Service) native.WorkspaceBindings {
	return native.WorkspaceBindings{
		RepositoryWatch: &native.RepositoryWatch{
			Open: func(ctx context.Context, root string) (native.RepositoryWatchFilter, []string, error) {
				return repo.NewWatchFilter(ctx, root)
			},
			Invalidate: repo.Invalidate,
		},
		CaptureBoundary: repo.CaptureManaged,
		ProjectSources: map[string]native.ProjectSourcePreparer{
			"clone": func(ctx context.Context, path string, source protocol.ProjectsSource) (string, error) {
				input := source.Data.(*protocol.ProjectsSourceClonePayload)
				return repo.Clone(ctx, input.Url, path, input.Branch)
			},
		},
		StageTurnHunk: func(ctx context.Context, root string, input protocol.ConversationCheckpointHunkSelection, transform func(string) (string, error)) (protocol.Output, error) {
			if input.ExpectedIndex == nil {
				return protocol.Output{}, invalid("staging a hunk requires the index revision")
			}
			index, err := repo.EditIndex(ctx, root, input.Path, *input.ExpectedIndex, input.ExpectedHead, input.ExpectedRevision, transform)
			value := protocol.ConversationCheckpointHunkApplied{Session: input.Session, Turn: input.Turn, Path: input.Path, Hunk: input.Hunk, Revision: input.ExpectedRevision, Index: &index}
			return protocol.Output{Kind: "hunk_applied", Data: value}, err
		},
		Commands: map[string]native.WorkspaceCommand{
			"inspect_git": readCommand(repositoryReadTimeout, true, func(ctx context.Context, root string, _ *protocol.CommandInspectGitPayload) (protocol.Output, error) {
				return output("git_status")(repo.Status(ctx, root))
			}),
			"read_git_head": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, _ *protocol.CommandReadGitHeadPayload) (protocol.Output, error) {
				return output("git_head")(repo.Head(ctx, root))
			}),
			"read_git_diff": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, v *protocol.CommandReadGitDiffPayload) (protocol.Output, error) {
				return output("git_diff")(repo.Diff(ctx, root, v.Path, v.Scope))
			}),
			"read_git_log": readCommand(repositoryReadTimeout, false, result("git_log", repo.Log)),
			"read_git_commit": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, v *protocol.CommandReadGitCommitPayload) (protocol.Output, error) {
				return output("git_commit")(repo.Commit(ctx, root, v.Commit))
			}),
			"read_git_show": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, v *protocol.CommandReadGitShowPayload) (protocol.Output, error) {
				return repo.Show(ctx, root, *v)
			}),
			"read_git_blame": readCommand(repositoryReadTimeout, false, result("git_blame", repo.Blame)),
			"list_git_branches": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, _ *protocol.CommandListGitBranchesPayload) (protocol.Output, error) {
				return output("git_branches")(repo.Branches(ctx, root))
			}),
			"resolve_git_revision": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, v *protocol.CommandResolveGitRevisionPayload) (protocol.Output, error) {
				commit, err := repo.ResolveRevision(ctx, root, v.Revision)
				return protocol.Output{Kind: "git_revision", Data: protocol.OutputGitRevisionPayload{Commit: commit}}, err
			}),
			"read_git_stash": readCommand(repositoryReadTimeout, false, func(ctx context.Context, root string, v *protocol.CommandReadGitStashPayload) (protocol.Output, error) {
				return output("git_diff")(repo.StashDiff(ctx, root, v.Commit))
			}),
			"read_git_output": readCommand(repositoryReadTimeout, false, func(_ context.Context, root string, _ *protocol.CommandReadGitOutputPayload) (protocol.Output, error) {
				return protocol.Output{Kind: "git_output", Data: protocol.OutputGitOutputPayload{Text: repo.Output(root)}}, nil
			}),
			"list_git_remote_tags": readCommand(remoteReadTimeout, false, func(ctx context.Context, root string, v *protocol.CommandListGitRemoteTagsPayload) (protocol.Output, error) {
				return output("git_remote_tags")(repo.RemoteTags(ctx, root, v.Remote))
			}),
			"list_git_changes": readCommand(repositoryReadTimeout, false, result("git_changes", repo.Changes)),
			"read_git_change":  readCommand(repositoryReadTimeout, false, result("git_change", repo.Change)),
			"read_git_context": readCommand(repositoryReadTimeout, false, result("git_context", repo.Context)),
			"apply_git_hunk": {Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
				input := command.Data.(*protocol.CommandApplyGitHunkPayload)
				if err := scope.ProtectEntry(input.Path); err != nil {
					return protocol.Output{}, err
				}
				defer scope.Changed()
				return output("git_hunk_applied")(repo.ApplyHunk(ctx, scope.Root(), *input))
			}},
			"update_git_index":  writeCommand(native.WorkspaceLockEffect, result("git_index", repo.UpdateIndex)),
			"create_git_commit": writeCommand(native.WorkspaceLockLocal, result("git_commit_created", repo.CreateCommit)),
			"create_git_branch": writeCommand(native.WorkspaceLockEffect, func(ctx context.Context, root string, v *protocol.CommandCreateGitBranchPayload) (protocol.Output, error) {
				return output("git_branch_created")(repo.CreateBranch(ctx, root, v.Name, v.Commit))
			}),
			"rename_git_branch": writeCommand(native.WorkspaceLockEffect, func(ctx context.Context, root string, v *protocol.CommandRenameGitBranchPayload) (protocol.Output, error) {
				return output("git_branch_renamed")(repo.RenameBranch(ctx, root, v.Name, v.NewName, v.Commit))
			}),
			"delete_git_branch": writeCommand(native.WorkspaceLockEffect, func(ctx context.Context, root string, v *protocol.CommandDeleteGitBranchPayload) (protocol.Output, error) {
				return protocol.Output{Kind: "git_branch_deleted", Data: protocol.OutputGitBranchDeletedPayload{Name: v.Name}}, repo.DeleteBranch(ctx, root, *v)
			}),
			"switch_git_branch": writeCommand(native.WorkspaceLockEffect, result("git_branch_switched", repo.SwitchBranch)),
			"merge_git_branch":  writeCommand(native.WorkspaceLockEffect, result("git_merged", repo.MergeBranch)),
			"run_git_action": writeCommand(native.WorkspaceLockLocal, func(ctx context.Context, root string, v *protocol.CommandRunGitActionPayload) (protocol.Output, error) {
				return protocol.Output{Kind: "git_action_completed"}, repo.Action(ctx, root, *v)
			}),
		},
	}
}

func output(kind string) func(any, error) (protocol.Output, error) {
	return func(value any, err error) (protocol.Output, error) {
		return protocol.Output{Kind: kind, Data: value}, err
	}
}

func result[I, O any](kind string, run func(context.Context, string, I) (O, error)) func(context.Context, string, *I) (protocol.Output, error) {
	return func(ctx context.Context, root string, input *I) (protocol.Output, error) {
		return output(kind)(run(ctx, root, *input))
	}
}

func readCommand[I any](timeout time.Duration, status bool, run func(context.Context, string, *I) (protocol.Output, error)) native.WorkspaceCommand {
	return native.WorkspaceCommand{Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
		root := scope.Root()
		return git.ReadTimed(ctx, timeout, status, func(ctx context.Context) (protocol.Output, error) {
			return run(ctx, root, command.Data.(*I))
		})
	}}
}

func writeCommand[I any](lock native.WorkspaceLock, run func(context.Context, string, *I) (protocol.Output, error)) native.WorkspaceCommand {
	return native.WorkspaceCommand{Lock: lock, Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
		scope.Changed()
		defer scope.Changed()
		return run(ctx, scope.Root(), command.Data.(*I))
	}}
}
