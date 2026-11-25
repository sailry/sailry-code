package desktop

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestDatabaseReceiptUsesOriginalRequestWithoutReplaying(t *testing.T) {
	var sent protocol.RequestID
	var runs int
	node := protocol.NodeID{1}
	r := &requests{drafts: make(map[protocol.RequestID]requestDraft), services: view.Services{
		Prepare: func(command protocol.Command) protocol.Request { return protocol.NewRequest(node, command) },
		Settle: func(_ context.Context, request protocol.Request) (protocol.Output, error) {
			runs++
			sent = request.ID
			if request.Target != node || request.Plugin != nil || request.Command.Kind != "query_database" {
				t.Fatalf("unexpected request: %+v", request)
			}
			return protocol.Output{}, &protocol.Fault{Code: protocol.ErrorCodeOutcomeUnknown, Message: "Fixture lost the reply"}
		},
		Resolve: func(_ context.Context, request protocol.Request) (view.Receipt, error) {
			if request.ID != sent || request.Target != node {
				t.Fatalf("inspection changed request: %+v", request)
			}
			return view.Receipt{State: "applied", Output: protocol.Output{Kind: "database_outcome", Data: protocol.DatabaseOutcome{
				Kind: "query", Data: protocol.DatabaseResultSet{Columns: []string{"large"}, Rows: [][]protocol.DatabaseValue{{{Kind: "integer", Data: int64(math.MaxInt64)}}}},
			}}}, nil
		},
	}}
	args := []json.RawMessage{json.RawMessage(`{"kind":"query_database","data":{"read_only":true,"database":null,"profile":"` + protocol.NewDatabaseID().String() + `","expected_revision":"18446744073709551615","sql":"SELECT 1","row_limit":1000,"timeout_ms":30000}}`)}
	id, err := r.prepareRequest(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	identity := []json.RawMessage{id}
	if reply, err := r.complete(t.Context(), identity); err != nil || !strings.Contains(string(reply), `"code":"outcome_unknown"`) {
		t.Fatalf("first execution = %s, %v", reply, err)
	}
	if _, err := r.complete(t.Context(), identity); err == nil {
		t.Fatal("the same request draft was redispatched")
	}
	if receipt, err := r.outcome(t.Context(), identity); err != nil || !strings.Contains(string(receipt), `"kind":"completed"`) || !strings.Contains(string(receipt), `"value":"9223372036854775807"`) {
		t.Fatalf("inspection did not return exact SQLite data: %s, %v", receipt, err)
	}
	if runs != 1 {
		t.Fatalf("expected one dispatched request, got %d", runs)
	}
	foreign := []json.RawMessage{json.RawMessage(`{"kind":"cancel_database","data":{"request":"` + protocol.NewRequestID().String() + `"}}`)}
	if _, err := r.prepareRequest(t.Context(), foreign); err == nil {
		t.Fatal("the database view prepared cancellation of an unrelated request")
	}
}

func TestDatabasePreparedConnectionUsesNativeSecretHandle(t *testing.T) {
	const password = "sqlite-test-never-printed"
	var captured protocol.Command
	r := &requests{drafts: make(map[protocol.RequestID]requestDraft), services: view.Services{
		Prepare: func(command protocol.Command) protocol.Request {
			captured = command
			return protocol.NewRequest(protocol.NodeID{1}, command)
		},
	}, credential: func(value string) (protocol.Secret, error) {
		if value != "protected-handle" {
			t.Fatalf("not a protected handle: %q", value)
		}
		return protocol.NewSecret(password), nil
	}}
	args := []json.RawMessage{json.RawMessage(`{"profile":{"id":"` + protocol.NewDatabaseID().String() + `","revision":"18446744073709551615","name":"fixture","read_only":true,"connection":{"kind":"network","engine":"mysql","host":"fixture.invalid","port":3306,"database":"fixture","username":"fixture","tls":"require"}},"testing":true,"secret":"protected-handle"}`)}
	draft, err := r.prepareDatabase(t.Context(), args)
	if err != nil || strings.Contains(string(draft), password) {
		t.Fatalf("the connection draft did not return a protected request identity: %v", err)
	}
	command, ok := captured.Data.(protocol.CommandTestDatabasePayload)
	if !ok || captured.Kind != "test_database" || command.ExpectedRevision != math.MaxUint64 || command.Password == nil || command.Password.Expose() != password {
		t.Fatal("protected credential or exact revision did not reach the native test command")
	}
}
