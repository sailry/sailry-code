package desktop

import (
	"encoding/json"
	"log/slog"
	"maps"
	"slices"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type gitMutation struct {
	command protocol.Command
	kind    string
	done    func(*ui.Context, protocol.Output)
	failed  []func(*ui.Context, error)
	running bool
}

func (p *repository) mutateNamed(kind string, command protocol.Command, done func(*ui.Context, protocol.Output), failed ...func(*ui.Context, error)) {
	encoded, err := json.Marshal(command)
	if err != nil {
		return
	}
	key := string(encoded)
	if p.mutations == nil {
		p.mutations = make(map[string]*gitMutation)
	}
	operation := p.mutations[key]
	if operation != nil && operation.running {
		return
	}
	if operation == nil {
		operation = &gitMutation{command: command, kind: kind, done: done, failed: failed}
		p.mutations[key] = operation
	}
	p.runMutation(key, operation)
}

func (p *repository) runMutation(key string, operation *gitMutation) {
	if operation.running || p.mutations[key] != operation {
		return
	}
	operation.running = true
	tracked := operation.command.Kind == "run_git_action"
	if tracked {
		p.running++
	}
	p.host.Mutate(operation.command, func(c *ui.Context, out protocol.Output, err error) {
		operation.running = false
		if tracked {
			p.running--
		}
		retained := native.MutationRetained(err)
		p.SetPromptUncertain(retained)
		if !retained {
			delete(p.mutations, key)
		}
		if err != nil {
			code, message := p.host.Fault(err)
			outcome, label := mutationFailure(operation.kind, code, message)
			kit.Toast(c, outcome, i18n.T(c, label))
			kit.RecordFailure(operation.command.Kind, outcome, err, slog.String("stage", "workspace_mutation"), slog.String("reason", label))
			for _, notify := range operation.failed {
				notify(c, err)
			}
			out = protocol.Output{}
		}
		if operation.done != nil {
			operation.done(c, out)
		} else if err == nil {
			kit.Toast(c, kit.Done, i18n.T(c, "workspace_completed"))
		}
	})
}

func (p *repository) pendingGitOperations() []native.WorkspaceOperation {
	var choices []native.WorkspaceOperation
	for _, key := range slices.Sorted(maps.Keys(p.mutations)) {
		operation := p.mutations[key]
		if operation.running || operation.kind == gitSwitch {
			continue
		}
		choices = append(choices, native.WorkspaceOperation{Label: "files_check_result", Run: func(*ui.Context) { p.runMutation(key, operation) }})
	}
	return choices
}
