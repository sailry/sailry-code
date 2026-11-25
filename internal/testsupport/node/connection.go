package nodetest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/sailry/sailry-harness/sdk/native/link"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	"lukechampine.com/blake3"
)

// HeldModel keeps its turn running until the test releases it, so
// connection tools can be bound and called against a live captured turn.
type HeldModel struct {
	fantasy.LanguageModel
	once    sync.Once
	Started chan struct{}
	Release chan struct{}
}

func (held *HeldModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	held.once.Do(func() { close(held.Started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-held.Release:
	}
	return func(yield func(fantasy.StreamPart) bool) {
		if yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "answer", Delta: "Done"}) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
		}
	}, nil
}

// SaveConnection uses an approval already claimed by the agent loop,
// as the native tool does. Preflight must stay read-only; its proof belongs to
// the committed save and must disappear when the save fails.
func SaveConnection(t *testing.T, node *native.Node, scope protocol.PluginContext, declaration sdktest.Declaration, command protocol.Command, checkRedacted func(protocol.Command)) link.Response {
	t.Helper()
	ctx := t.Context()
	request := protocol.NewRequest(node.ID(), command)
	request.Plugin = &scope
	binding := sdktest.ApprovalBinding{Owner: scope.Package, Tool: declaration.Name, Operation: &declaration.Operation}
	approval := ClaimApproval(t, node, request, binding)
	if err := sdktest.Inspect(node).ValidatePluginRequest(ctx, node.ID(), request); err != nil {
		t.Fatalf("approved save failed read-only preflight: %v", err)
	}
	prepared := func() string {
		t.Helper()
		var body string
		if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
			return db.QueryRowContext(ctx, "SELECT prepared FROM agent_approvals WHERE id=?1", approval.String()).Scan(&body)
		}); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if body := prepared(); body != "null" {
		t.Fatalf("preflight wrote a save proof: %s", body)
	}
	response := Dispatch(t, node, node.ID(), request)
	if body := prepared(); response.Fault != nil {
		if body != "null" {
			t.Fatalf("failed save retained its proof: %s", body)
		}
	} else {
		var proof struct {
			Command protocol.Command `json:"command"`
			Digest  string           `json:"digest"`
		}
		if err := json.Unmarshal([]byte(body), &proof); err != nil || proof.Command.Kind != command.Kind || proof.Digest == "" {
			t.Fatalf("committed save lost its proof: %s, %v", body, err)
		}
		encoded, err := json.Marshal(command)
		if err != nil || proof.Digest != fmt.Sprintf("%x", blake3.Sum256(encoded)) {
			t.Fatalf("save proof did not bind the original credential-bearing command: %v", err)
		}
		checkRedacted(proof.Command)
		replayed := Dispatch(t, node, node.ID(), request)
		first, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		again, err := json.Marshal(replayed)
		if err != nil || string(first) != string(again) || body != prepared() {
			t.Fatalf("save replay changed its result or proof: %s, %s, %v", first, again, err)
		}
	}
	return response
}

func ClaimApproval(t *testing.T, node *native.Node, request protocol.Request, binding sdktest.ApprovalBinding) protocol.ApprovalID {
	t.Helper()
	ctx := t.Context()
	approval := protocol.DefaultConversationApproval()
	approval.ID, approval.Session, approval.Turn = protocol.NewApprovalID(), *request.Plugin.Session, *request.Plugin.Turn
	approval.State, approval.Source = protocol.ConversationApprovalStateApproved, protocol.ConversationApprovalSourceUser
	if err := sdktest.Inspect(node).Store().Node.Write(ctx, func(db *sql.Conn) error {
		body, err := json.Marshal(approval)
		if err != nil {
			return err
		}
		bound, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, "INSERT INTO agent_approvals(id,turn,call,body,binding,request,claimed) VALUES(?1,?2,?3,?4,?5,?6,1)", approval.ID.String(), approval.Turn.String(), request.ID.String(), body, bound, request.ID.String())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return approval.ID
}

func ConnectionTools(t *testing.T, node *native.Node, inv *native.AgentInvocation) map[protocol.ToolOperation]native.Tool {
	t.Helper()
	tools, err := sdktest.Inspect(node).ModuleTools(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	bound := map[protocol.ToolOperation]native.Tool{}
	for _, tool := range tools {
		if tool.Binding != nil && tool.Binding.Operation != nil && sdktest.ConnectionOperation(*tool.Binding.Operation) {
			bound[*tool.Binding.Operation] = tool
		}
	}
	return bound
}

func ConnectionDeclarations(tools map[protocol.ToolOperation]native.Tool) map[protocol.ToolOperation]sdktest.Declaration {
	declarations := make(map[protocol.ToolOperation]sdktest.Declaration, len(tools))
	for op, tool := range tools {
		readOnly, concurrent := sdktest.Policy(op)
		declarations[op] = sdktest.Declaration{
			Spec: sdktest.Spec{Operation: op, ReadOnly: readOnly, Concurrent: concurrent},
			Name: tool.Definition.Name,
		}
	}
	return declarations
}

func HoldTurn(t *testing.T, node *native.Node, session protocol.Session, started <-chan struct{}) *native.AgentInvocation {
	t.Helper()
	turn := Command(t, node, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
		Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Inspect saved connections"},
	}}).Data.(protocol.QueuedTurn)
	select {
	case <-started:
	case <-time.After(Wait):
		t.Fatal("the held turn never reached its model")
	}
	inv := &native.AgentInvocation{Caller: node.ID(), Session: session, Turn: turn}
	for _, tree := range Snapshot(t, node).Worktrees {
		if tree.ID == session.Worktree {
			inv.Worktree = tree
			break
		}
	}
	if inv.Worktree.ID != session.Worktree {
		t.Fatal("the held turn has no worktree")
	}
	if err := sdktest.Inspect(node).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
		var err error
		inv.Run, err = sdktest.ReadAgentRun(t.Context(), db, turn.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if inv.Run.Status != protocol.ConversationStatusRunning {
		t.Fatalf("held turn status = %s", inv.Run.Status)
	}
	return inv
}
