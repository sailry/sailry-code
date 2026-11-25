package codehost_test

import (
	"context"
	"errors"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/sailry/sailry-code/internal/testsupport/node"
)

// agentToolScript is a fake model whose calls for each request come from the
// running subtest. It records the offered tools, the context and every result.
type agentToolScript struct {
	fantasy.LanguageModel
	mu      sync.Mutex
	offered [][]string
	prompts []string
	results map[string]string
	plan    func(step int, results map[string]string) [][3]string
}

func (script *agentToolScript) reset(plan func(int, map[string]string) [][3]string) {
	script.mu.Lock()
	defer script.mu.Unlock()
	script.offered, script.prompts, script.results, script.plan = nil, nil, map[string]string{}, plan
}

func (script *agentToolScript) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	script.mu.Lock()
	script.offered = append(script.offered, nodetest.CallableTools(call))
	var prompt strings.Builder
	for _, message := range call.Prompt {
		for _, part := range message.Content {
			if text, found := fantasy.AsMessagePart[fantasy.TextPart](part); found {
				prompt.WriteString(text.Text + "\n")
			}
			if result, found := fantasy.AsMessagePart[fantasy.ToolResultPart](part); found {
				if text, found := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Output); found {
					script.results[result.ToolCallID] = text.Text
				}
			}
		}
	}
	script.prompts = append(script.prompts, prompt.String())
	calls := script.plan(len(script.offered), script.results)
	script.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		for _, call := range calls {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: call[0], ToolCallName: call[1], ToolCallInput: call[2]}) {
				return
			}
		}
		if len(calls) == 0 && !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "answer", Delta: "Done"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

// mediaScript replays fixed tool calls for one conversation model and records
// what each request offered and returned.
type mediaScript struct {
	fantasy.LanguageModel
	mu      sync.Mutex
	steps   [][][3]string
	offered [][]string
	results map[string]string
}

func (script *mediaScript) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	script.mu.Lock()
	script.offered = append(script.offered, nodetest.CallableTools(call))
	step := len(script.offered)
	for _, message := range call.Prompt {
		for _, part := range message.Content {
			if result, found := fantasy.AsMessagePart[fantasy.ToolResultPart](part); found {
				switch output := result.Output.(type) {
				case fantasy.ToolResultOutputContentText:
					script.results[result.ToolCallID] = output.Text
				case fantasy.ToolResultOutputContentError:
					script.results[result.ToolCallID] = "error: " + output.Error.Error()
				}
			}
		}
	}
	script.mu.Unlock()
	if step > len(script.steps)+1 {
		return nil, errors.New("unexpected extra media model request")
	}
	var calls [][3]string
	if step <= len(script.steps) {
		calls = script.steps[step-1]
	}
	return func(yield func(fantasy.StreamPart) bool) {
		for _, call := range calls {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: call[0], ToolCallName: call[1], ToolCallInput: call[2]}) {
				return
			}
		}
		if len(calls) == 0 && !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "answer", Delta: "Done"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}
