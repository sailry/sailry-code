package nodetest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

type Step struct {
	ID, Tool  string
	Arguments func() string
}

type Script struct {
	fantasy.LanguageModel
	mu       sync.Mutex
	steps    []Step
	requests [][]string
	offered  []string
	results  map[string]string
}

func NewScript(steps ...Step) *Script { return &Script{steps: steps} }

func (script *Script) Reset(steps ...Step) {
	script.mu.Lock()
	defer script.mu.Unlock()
	script.steps, script.requests, script.offered, script.results = steps, nil, nil, nil
}

func (script *Script) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	script.mu.Lock()
	defer script.mu.Unlock()
	if len(script.requests) >= len(script.steps) {
		return nil, errors.New("unexpected extra acceptance model request")
	}
	step := script.steps[len(script.requests)]
	names := make([]string, 0, len(call.Tools))
	for _, tool := range call.Tools {
		names = append(names, tool.GetName())
	}
	script.requests = append(script.requests, names)
	script.offered = append(script.offered, CallableTools(call)...)
	if script.results == nil {
		script.results = map[string]string{}
	}
	for _, message := range call.Prompt {
		for _, part := range message.Content {
			if response, found := fantasy.AsMessagePart[fantasy.ToolResultPart](part); found {
				if text, found := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](response.Output); found {
					script.results[response.ToolCallID] = text.Text
				}
			}
		}
	}
	part := fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "answer", Delta: "Done"}
	if step.Tool != "" {
		part = fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: step.ID, ToolCallName: step.Tool, ToolCallInput: step.Arguments()}
	}
	return func(yield func(fantasy.StreamPart) bool) {
		if yield(part) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
		}
	}, nil
}

func (script *Script) Requests() [][]string {
	script.mu.Lock()
	defer script.mu.Unlock()
	return slices.Clone(script.requests)
}

func (script *Script) Observation(id string) (string, []string) {
	script.mu.Lock()
	defer script.mu.Unlock()
	return script.results[id], slices.Clone(script.offered)
}

// DeclaredToolName is the name a model reads for a package tool's alias when
// no other captured tool declares the same name.
func DeclaredToolName(alias string) string {
	if rest, found := strings.CutPrefix(alias, "plugin_"); found && len(rest) > 17 && rest[16] == '_' {
		return rest[17:]
	}
	return alias
}

var loadedToolPattern = regexp.MustCompile(`<loaded-tool name="([^"]+)">`)

// CallableTools lists the tools a request lets the model call: its
// declarations, then the loaded deferred tools its context describes, which
// the model runs through call_tool without a change to the declarations.
func CallableTools(call fantasy.Call) []string {
	var names []string
	for _, tool := range call.Tools {
		names = append(names, tool.GetName())
	}
	for _, message := range call.Prompt {
		for _, part := range message.Content {
			if text, found := fantasy.AsMessagePart[fantasy.TextPart](part); found {
				for _, match := range loadedToolPattern.FindAllStringSubmatch(text.Text, -1) {
					if !slices.Contains(names, match[1]) {
						names = append(names, match[1])
					}
				}
			}
		}
	}
	return names
}

func Arguments(value any) func() string {
	return func() string {
		body, _ := json.Marshal(value)
		return string(body)
	}
}

// Start starts a real Node with an optional fixture model.
func Start(t *testing.T, profile string, script fantasy.LanguageModel, options native.Options) *native.Node {
	t.Helper()
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
	options.Environment = &environment
	if options.MCP == nil {
		options.MCP = sdktest.MCPOptions()
	}
	if script != nil {
		options.Model = func(context.Context, sdktest.Binding) (fantasy.LanguageModel, error) { return script, nil }
	}
	node, err := native.StartWithOptions(t.Context(), profile, options)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func SessionConfig(t *testing.T, node *native.Node, contextSize uint32, permission protocol.Permission) protocol.SessionConfig {
	t.Helper()
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Revision, provider.Enabled, provider.Name = protocol.NewProviderID(), 1, true, "Fixture"
	provider.Api, provider.Authentication = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey
	provider.Endpoint, provider.DefaultModel = "http://fixture.invalid/v1", "fixture"
	selected := protocol.DefaultConversationModel()
	selected.ID, selected.Context, selected.Output, selected.Tools = "fixture", contextSize, 8000, true
	provider.Models = []protocol.ConversationModel{selected}
	ctx := t.Context()
	if err := sdktest.Inspect(node).Store().Node.Write(ctx, func(db *sql.Conn) error {
		body, err := json.Marshal(map[string]any{"id": provider.ID, "provider": provider, "listed": true, "authorizations": map[string]any{}, "quota": nil})
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, "INSERT INTO providers(id,body) VALUES(?1,?2)", provider.ID.String(), body)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Permission, config.Mode = provider.ID, "fixture", permission, protocol.WorkModeCode
	return config
}

func ProjectSession(t *testing.T, node *native.Node, project protocol.ProjectID, permission protocol.Permission) protocol.Session {
	t.Helper()
	config := SessionConfig(t, node, 200000, permission)
	return Do[protocol.Session](t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project, Config: &config}})
}

// Turn submits one message and runs the turn to completion within
// wait. Every pending approval is handed to approve before it is approved. It
// returns the packages frozen for the turn and the tool responses by call.
func Turn(t *testing.T, node *native.Node, session protocol.Session, text string, wait time.Duration, approve func(protocol.ConversationApproval)) ([]protocol.PluginReference, map[string]*sdktest.ToolResponse) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	subscription, err := node.Subscribe(ctx, node.ID(), protocol.Topic{Kind: "node"})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	queued := Do[protocol.QueuedTurn](t, node, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
		Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: text},
	}})
	seen := map[protocol.ApprovalID]bool{}
	for completed := false; !completed; {
		var history protocol.ConversationHistory
		if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
			var err error
			history, err = sdktest.ReadConversation(ctx, db, session.ID, nil, 100)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, approval := range history.Page.Approvals {
			if approval.State != protocol.ConversationApprovalStatePending || seen[approval.ID] {
				continue
			}
			seen[approval.ID] = true
			if approve == nil {
				t.Fatalf("the turn asked for an unexpected approval: %+v", approval)
			}
			approve(approval)
			Do[json.RawMessage](t, node, protocol.Command{Kind: "resolve_approval", Data: protocol.CommandResolveApprovalPayload{
				Session: session.ID, Approval: approval.ID, Decision: protocol.ConversationDecision{Kind: "approve"},
			}})
		}
		for _, run := range history.Page.Runs {
			switch run.Status {
			case protocol.ConversationStatusCompleted:
				completed = true
			case protocol.ConversationStatusFailed, protocol.ConversationStatusInterrupted, protocol.ConversationStatusCancelled:
				t.Fatalf("turn failed: %+v", run)
			}
		}
		if !completed {
			if _, err := subscription.Next(ctx); err != nil {
				t.Fatalf("turn did not complete within %s: %v", wait, err)
			}
		}
	}
	var state sdktest.AgentState
	if err := sdktest.Inspect(node).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
		var err error
		state, err = sdktest.LoadAgentState(t.Context(), db, session.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	results := map[string]*sdktest.ToolResponse{}
	for _, record := range state.Records {
		if message := record.Message(); message != nil {
			for _, block := range message.Content {
				if response, ok := block.Data.(*sdktest.ToolResponse); ok {
					results[response.ID] = response
				}
			}
		}
	}
	return queued.Plugins, results
}

// Result decodes a successful tool response.
func Result(t *testing.T, results map[string]*sdktest.ToolResponse, call, tool string) map[string]any {
	t.Helper()
	response := results[call]
	if response == nil {
		t.Fatalf("%s returned no response", call)
	}
	if response.Name != tool || response.Status != sdktest.ToolSuccess {
		t.Fatalf("%s (%s) status %v: %s", call, response.Name, response.Status, response.Result.Value)
	}
	var value map[string]any
	if err := json.Unmarshal(response.Result.Value, &value); err != nil {
		t.Fatalf("%s result %s: %v", call, response.Result.Value, err)
	}
	return value
}
