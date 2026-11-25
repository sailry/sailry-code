package databasesnode_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	databasesnode "github.com/sailry/sailry-code/modules/code/databases/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestDatabaseConnectionTools(t *testing.T) {
	t.Parallel()
	t.Run("arguments", func(t *testing.T) {
		id := protocol.NewDatabaseID()
		for _, entry := range []struct {
			name      string
			operation protocol.ToolOperation
			arguments string
			message   string
		}{
			{"query requires a limit", protocol.ToolOperationQueryDatabase, `{"connection":"` + id.String() + `","sql":"select 1","timeout_ms":1}`, "invalid connection operation: "},
			{"query rejects unknown fields", protocol.ToolOperationQueryDatabase, `{"connection":"` + id.String() + `","sql":"select 1","row_limit":1,"timeout_ms":1,"password":"x"}`, "invalid connection operation: "},
			{"save rejects null edits", protocol.ToolOperationSaveDatabaseConnection, `{"name":null}`, "connection edit fields cannot be null"},
		} {
			_, err := sdktest.ConnectionCall(entry.operation, json.RawMessage(entry.arguments))
			var fault *protocol.Fault
			if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodeInvalidRequest || !strings.HasPrefix(fault.Message, entry.message) {
				t.Errorf("%s: error = %v", entry.name, err)
			}
		}
		input, err := sdktest.ConnectionCall(protocol.ToolOperationSaveDatabaseConnection, json.RawMessage(`{"connection":null,"expected_revision":null,"name":"Local"}`))
		if err != nil || string(input.Save) != `{"connection":null,"expected_revision":null,"name":"Local"}` || input.Target != nil {
			t.Errorf("save arguments = %#v %v", input, err)
		}
		input, err = sdktest.ConnectionCall(protocol.ToolOperationBrowseDatabase, json.RawMessage(`{"connection":"`+id.String()+`","database":null}`))
		if err != nil || input.Target == nil || input.Target.Kind != "database" || input.Target.Data != any(id) || input.Database != nil {
			t.Errorf("catalog arguments = %#v %v", input, err)
		}
	})

	held := &nodetest.HeldModel{Started: make(chan struct{}), Release: make(chan struct{})}
	node := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), held, native.Options{Assembly: native.Assembly{Modules: []native.Module{databasesnode.Module()}}})
	t.Cleanup(func() {
		close(held.Release)
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	project := nodetest.Command(t, node, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: "Fixture", Path: t.TempDir(), Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
	}}).Data.(protocol.Project)
	const databasePassword = "fixture-database-password"
	network := func(host string) protocol.DatabaseConnection {
		return protocol.DatabaseConnection{Kind: "network", Data: &protocol.DatabaseConnectionNetworkPayload{
			Engine: protocol.DatabaseEnginePostgres, Host: host, Port: 5432, Database: "app", Username: "reader", Tls: protocol.DatabaseTlsRequire}}
	}
	saveDatabase := func(name, host string, sharing *protocol.ConnectionSharing) protocol.DatabaseProfile {
		password := protocol.NewSecret(databasePassword)
		output := nodetest.Command(t, node, protocol.Command{Kind: "save_database", Data: protocol.CommandSaveDatabasePayload{
			Profile: protocol.DatabaseProfile{ID: protocol.NewDatabaseID(), Name: name, Connection: network(host), ReadOnly: true, Sharing: sharing}, Password: &password}})
		profile, valid := nodetest.Payload[protocol.DatabaseProfile](output.Data)
		if !valid {
			t.Fatalf("saved database = %#v", output)
		}
		return profile
	}
	shared := saveDatabase("Shared", "shared.fixture.test", &protocol.ConnectionSharing{Kind: "global"})
	private := saveDatabase("Private", "private.fixture.test", nil)
	config := nodetest.SessionConfig(t, node, 32000, protocol.PermissionAsk)
	session := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project.ID, Config: &config}}).Data.(protocol.Session)
	ctx := t.Context()
	inv := nodetest.HoldTurn(t, node, session, held.Started)
	bound := nodetest.ConnectionTools(t, node, inv)
	declarations := nodetest.ConnectionDeclarations(bound)
	captured := []protocol.ConnectionResource{{Kind: "database", Data: shared.ID}}

	t.Run("commands", func(t *testing.T) {
		database := "reports"
		for _, entry := range []struct {
			operation protocol.ToolOperation
			arguments map[string]any
			command   protocol.Command
		}{
			{protocol.ToolOperationListDatabaseConnections, map[string]any{}, protocol.Command{Kind: "list_databases"}},
			{protocol.ToolOperationBrowseDatabase, map[string]any{"connection": shared.ID, "database": database},
				protocol.Command{Kind: "browse_database", Data: &protocol.CommandBrowseDatabasePayload{Profile: shared.ID, ExpectedRevision: shared.Revision, Database: &database}}},
			{protocol.ToolOperationQueryDatabase, map[string]any{"connection": shared.ID, "sql": "select 1", "row_limit": 1000, "timeout_ms": 30000},
				protocol.Command{Kind: "query_database", Data: &protocol.CommandQueryDatabasePayload{ReadOnly: true, Profile: shared.ID, ExpectedRevision: shared.Revision, Sql: "select 1", RowLimit: 1000, TimeoutMs: 30000}}},
			{protocol.ToolOperationExecuteDatabase, map[string]any{"connection": shared.ID, "database": database, "sql": "delete from logs", "row_limit": 10, "timeout_ms": 5000},
				protocol.Command{Kind: "query_database", Data: &protocol.CommandQueryDatabasePayload{Database: &database, Profile: shared.ID, ExpectedRevision: shared.Revision, Sql: "delete from logs", RowLimit: 10, TimeoutMs: 5000}}},
		} {
			arguments, err := json.Marshal(entry.arguments)
			if err != nil {
				t.Fatal(err)
			}
			command, err := sdktest.Inspect(node).ConnectionCommand(ctx, inv, declarations[entry.operation], arguments, captured)
			if err != nil || !reflect.DeepEqual(command, entry.command) {
				t.Errorf("%s command = %#v %v", entry.operation, command, err)
			}
		}
	})

	t.Run("sharing", func(t *testing.T) {
		for _, entry := range []struct {
			name      string
			operation protocol.ToolOperation
			arguments map[string]any
			captured  []protocol.ConnectionResource
		}{
			{"unshared database", protocol.ToolOperationQueryDatabase, map[string]any{"connection": private.ID, "sql": "select 1", "row_limit": 1, "timeout_ms": 1000}, captured},
			{"unshared database named as captured", protocol.ToolOperationQueryDatabase, map[string]any{"connection": private.ID, "sql": "select 1", "row_limit": 1, "timeout_ms": 1000},
				append(slices.Clone(captured), protocol.ConnectionResource{Kind: "database", Data: private.ID})},
			{"shared database outside the capture", protocol.ToolOperationBrowseDatabase, map[string]any{"connection": shared.ID}, nil},
		} {
			arguments, err := json.Marshal(entry.arguments)
			if err != nil {
				t.Fatal(err)
			}
			command, err := sdktest.Inspect(node).ConnectionCommand(ctx, inv, declarations[entry.operation], arguments, entry.captured)
			var fault *protocol.Fault
			if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodePermissionDenied {
				t.Errorf("%s: command = %#v, error = %v", entry.name, command, err)
			}
		}
		tool := bound[protocol.ToolOperationQueryDatabase]
		arguments, _ := json.Marshal(map[string]any{"connection": private.ID, "sql": "select 1"})
		err := tool.Prepare(ctx, native.ToolRequest{ID: "refused", Name: tool.Definition.Name, Arguments: arguments})
		var fault *protocol.Fault
		if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodeInvalidRequest {
			t.Errorf("prepared an unshared connection: %v", err)
		}
		if result := tool.Execute(ctx, native.ToolRequest{ID: "refused", Name: tool.Definition.Name, Arguments: arguments}); result.Failure == nil {
			t.Error("executed a call that was never prepared")
		}
	})

	t.Run("results", func(t *testing.T) {
		encode := func(value any) string {
			t.Helper()
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if text := string(body); strings.Contains(text, databasePassword) || strings.Contains(text, `"password":`) {
				t.Fatalf("model-visible result carries a secret: %s", text)
			}
			return string(body)
		}
		owner := declarations[protocol.ToolOperationListDatabaseConnections].Owner
		value, err := sdktest.Inspect(node).ConnectionResult(ctx, inv, owner, protocol.Output{Kind: "database_profiles", Data: []protocol.DatabaseProfile{private, shared}}, captured)
		if text := encode(value); err != nil || strings.Contains(text, private.ID.String()) || !strings.Contains(text, shared.ID.String()) {
			t.Errorf("filtered list = %s %v", text, err)
		}
		value, err = sdktest.Inspect(node).ConnectionResult(ctx, inv, owner, protocol.Output{Kind: "database_profile", Data: shared}, captured)
		if text := encode(value); err != nil || !strings.Contains(text, `"credentials_required":false`) || !strings.Contains(text, `"kind":"database_profile"`) {
			t.Errorf("saved profile = %s %v", text, err)
		}
		_, err = sdktest.Inspect(node).ConnectionResult(ctx, inv, owner, protocol.Output{Kind: "database_profile", Data: private}, captured)
		var fault *protocol.Fault
		if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodePermissionDenied {
			t.Errorf("unreachable saved profile: %v", err)
		}
	})
	t.Run("database save proof", func(t *testing.T) { testDatabaseSaveProof(t, node, inv, declarations) })
}
