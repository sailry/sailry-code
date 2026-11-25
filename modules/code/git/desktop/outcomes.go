package desktop

import (
	"strings"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func mutationFailure(kind string, code protocol.ErrorCode, message string) (kit.Outcome, string) {
	if code == protocol.ErrorCodeUnavailable && message == "resource host is offline" {
		return kit.Failed, "resource_offline_action"
	}
	outcome := kit.Failed
	switch code {
	case protocol.ErrorCodeConflict, protocol.ErrorCodeRevisionConflict:
		outcome = kit.Conflict
	case protocol.ErrorCodeOutcomeUnknown:
		outcome = kit.Uncertain
	}
	conflict := code == protocol.ErrorCodeConflict
	switch kind {
	case gitIndex:
		switch code {
		case protocol.ErrorCodeRevisionConflict:
			return outcome, "git_index_changed"
		case protocol.ErrorCodeBusy:
			return outcome, "git_index_busy"
		case protocol.ErrorCodePermissionDenied:
			return outcome, "git_index_denied"
		case protocol.ErrorCodeNotFound:
			return outcome, "git_index_missing"
		case protocol.ErrorCodeOutcomeUnknown:
			return outcome, "git_index_unknown"
		}
		return outcome, "git_index_failed"
	case workspaceAction:
		switch outcome {
		case kit.Conflict:
			return outcome, "workspace_changed"
		case kit.Uncertain:
			return outcome, "workspace_action_unknown"
		}
		return outcome, "workspace_action_failed"
	}
	branch := kind == gitSwitch || kind == gitDelete || kind == gitMerge
	switch {
	case code == protocol.ErrorCodeRevisionConflict && branch:
		return outcome, "git_branch_revision"
	case code == protocol.ErrorCodeRevisionConflict:
		return outcome, "git_index_conflict"
	case code == protocol.ErrorCodeNotConfigured && (kind == gitCommit || kind == gitMerge):
		return outcome, "git_identity_missing"
	case code == protocol.ErrorCodeUnavailable && (kind == gitCommit || kind == gitMerge):
		return outcome, "git_commit_unavailable"
	case code == protocol.ErrorCodeBusy && (kind == gitSwitch || kind == gitMerge):
		return outcome, "git_branch_busy"
	case conflict && message == protocol.GitFaultBranchInWorktree:
		return outcome, "git_branch_checked_out"
	case conflict && message == protocol.GitFaultBranchExists:
		return outcome, "git_branch_exists"
	case conflict:
		switch kind {
		case gitSwitch:
			return outcome, "git_branch_conflict"
		case gitMerge:
			return outcome, "git_merge_conflict"
		case gitDelete:
			return outcome, "git_branch_delete_conflict"
		case gitRename:
			return outcome, "git_branch_rename_conflict"
		case gitCommit:
			return outcome, "git_commit_failed"
		}
		return outcome, "git_action_conflict"
	case code == protocol.ErrorCodePermissionDenied && kind == gitOperation:
		return outcome, "git_auth_failed"
	}
	prefix := "git_action"
	switch kind {
	case gitSwitch:
		prefix = "git_branch"
	case gitDelete:
		prefix = "git_branch_delete"
	case gitMerge:
		prefix = "git_merge"
	case gitCommit:
		prefix = "git_commit"
	case gitCreate:
		prefix = "git_branch_create"
	case gitRename:
		prefix = "git_branch_rename"
	}
	if code == protocol.ErrorCodeOutcomeUnknown {
		return outcome, prefix + "_unknown"
	}
	return outcome, prefix + "_failed"
}

func (p *repository) reportRead(c *ui.Context, key, operation string, err error) {
	if err == nil {
		delete(p.readReported, key)
		if len(p.readReported) == 0 && p.host.PublishInspectionNotice != nil {
			p.host.PublishInspectionNotice("")
		}
		return
	}
	code, message := p.host.Fault(err)
	outcome, label := mutationFailure(workspaceAction, code, message)
	if outcome == kit.Failed && label != "resource_offline_action" && strings.HasPrefix(key, "git-") {
		switch code {
		case protocol.ErrorCodeNotFound:
			label = "git_index_missing"
		case protocol.ErrorCodePermissionDenied:
			label = "git_index_denied"
		case protocol.ErrorCodeBusy:
			label = "git_index_busy"
		default:
			label = "git_inspection_failed"
		}
	}
	kit.RecordFailure(operation, outcome, err)
	if !strings.HasPrefix(key, "git-") {
		kit.Toast(c, outcome, i18n.T(c, label))
		return
	}
	told := p.host.LocationNotice != nil && p.host.LocationNotice() == label
	for _, reported := range p.readReported {
		if reported == label {
			told = true
			break
		}
	}
	if p.readReported == nil {
		p.readReported = make(map[string]string)
	}
	p.readReported[key] = label
	if p.host.PublishInspectionNotice != nil {
		p.host.PublishInspectionNotice(label)
	}
	if !told {
		kit.Toast(c, outcome, i18n.T(c, label))
	}
}
