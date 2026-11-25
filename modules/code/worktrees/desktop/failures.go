package worktreesdesktop

import (
	"errors"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

var (
	contextChanged  = protocol.Fault{Code: protocol.ErrorCodeConflict, Message: "the workspace location changed"}
	invalidWorktree = protocol.Fault{Code: protocol.ErrorCodeUnavailable, Message: "the registered workspace location is unavailable"}
)

func reportKey(workspace native.WorkflowWorkspace, c *ui.Context, key string, err error) {
	if err == nil {
		err = protocol.Fault{Code: protocol.ErrorCodeUnavailable}
	}
	workspace.Report(c, err, key, key, key)
}

func reportChange(workspace native.WorkflowWorkspace, c *ui.Context, action string, err error) {
	reportKey(workspace, c, worktreeFailure(action, err), err)
}

func worktreeFailure(action string, err error) string {
	var fault protocol.Fault
	if pointer, ok := errors.AsType[*protocol.Fault](err); ok && pointer != nil {
		fault = *pointer
	} else if value, ok := errors.AsType[protocol.Fault](err); ok {
		fault = value
	}
	if fault.Code == protocol.ErrorCodeOutcomeUnknown {
		return "worktree_outcome_unknown"
	}
	if fault.Code == protocol.ErrorCodeInvalidRequest {
		return "worktree_invalid"
	}
	conflict := fault.Code == protocol.ErrorCodeConflict || fault.Code == protocol.ErrorCodeRevisionConflict
	switch action {
	case "remove_worktree":
		switch {
		case fault.Code == protocol.ErrorCodeBusy && fault.Message == "worktree is locked":
			return "worktree_remove_locked"
		case fault.Code == protocol.ErrorCodeBusy:
			return "worktree_remove_busy"
		case fault.Code == protocol.ErrorCodeConflict && fault.Message == "worktree has uncommitted changes":
			return "worktree_remove_dirty"
		case conflict:
			return "worktree_remove_conflict"
		case fault.Code == protocol.ErrorCodeNotFound:
			return "worktree_remove_missing"
		}
	case "prune_worktree":
		switch {
		case fault.Code == protocol.ErrorCodeBusy && fault.Message == "worktree is locked":
			return "worktree_remove_locked"
		case fault.Code == protocol.ErrorCodeBusy:
			return "git_index_busy"
		case fault.Code == protocol.ErrorCodeConflict && fault.Message == "worktree path still exists":
			return "worktree_prune_exists"
		case conflict:
			return "git_index_conflict"
		case fault.Code == protocol.ErrorCodeNotFound:
			return "worktree_remove_missing"
		}
		return "worktree_prune_failed"
	default:
		switch {
		case fault.Code == protocol.ErrorCodeConflict && fault.Message == "branch already exists":
			return "git_branch_exists"
		case fault.Code == protocol.ErrorCodeConflict && fault.Message == "destination already exists":
			return "worktree_path_exists"
		case conflict:
			return "worktree_conflict"
		case fault.Code == protocol.ErrorCodeNotFound:
			return "worktree_not_found"
		}
	}
	return "worktree_request_failed"
}
