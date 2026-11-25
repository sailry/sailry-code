package nodetest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sailry/sailry-harness/sdk/native/link"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

const Wait = 15 * time.Second

func Dispatch(t *testing.T, node *native.Node, caller protocol.NodeID, request protocol.Request) link.Response {
	t.Helper()
	return dispatch(t, context.Background(), node, caller, request, 0)
}

func dispatch(t *testing.T, ctx context.Context, node *native.Node, caller protocol.NodeID, request protocol.Request, wait time.Duration) link.Response {
	t.Helper()
	accepted, err := node.Dispatch(ctx, caller, request)
	if err != nil {
		var fault *protocol.Fault
		if errors.As(err, &fault) {
			return link.Response{Fault: fault}
		}
		t.Fatalf("%s was not admitted: %v", request.Command.Kind, err)
	}
	var timeout <-chan time.Time
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case result, delivered := <-accepted.Completion:
		if !delivered {
			t.Fatalf("%s lost its completion", request.Command.Kind)
		}
		return result
	case <-timeout:
		t.Fatalf("%s did not complete within %s", request.Command.Kind, wait)
	}
	return link.Response{}
}

func Command(t *testing.T, node *native.Node, command protocol.Command) protocol.Output {
	t.Helper()
	value := Dispatch(t, node, node.ID(), protocol.NewRequest(node.ID(), command))
	if value.Fault != nil {
		t.Fatal(value.Fault)
	}
	return value.Output
}

func DecodeCommand(t *testing.T, kind string, data any) protocol.Command {
	t.Helper()
	body, err := json.Marshal(map[string]any{"kind": kind, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	var command protocol.Command
	if err := json.Unmarshal(body, &command); err != nil {
		t.Fatalf("decode %s: %v", kind, err)
	}
	return command
}

func Snapshot(t *testing.T, node *native.Node) protocol.Snapshot {
	t.Helper()
	value, err := node.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func Payload[Value any](input any) (Value, bool) {
	switch value := input.(type) {
	case Value:
		return value, true
	case *Value:
		if value != nil {
			return *value, true
		}
	}
	var empty Value
	return empty, false
}

func TrustCaller(t *testing.T, node *native.Node, caller protocol.NodeID) {
	t.Helper()
	if err := sdktest.Inspect(node).Store().Node.Write(t.Context(), func(db *sql.Conn) error {
		return sdktest.SetLinkTrust(t.Context(), db, caller, true)
	}); err != nil {
		t.Fatal(err)
	}
}

func InstallPlugin(t *testing.T, node *native.Node, name string, resources map[string]string) protocol.PluginInfo {
	t.Helper()
	source := t.TempDir()
	for resource, body := range resources {
		path := filepath.Join(source, filepath.FromSlash(resource))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := sdktest.Inspect(node).InstallPlugin(t.Context(), source, "", name)
	if err != nil || len(info.Issues) != 0 {
		t.Fatalf("%s fixture is invalid: %+v, %v", name, info.Issues, err)
	}
	if err := sdktest.Inspect(node).Store().Node.Write(t.Context(), func(db *sql.Conn) error {
		var err error
		info, err = sdktest.InstallPlugin(t.Context(), db, info, 0, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return info
}

func FaultIs(err error, code protocol.ErrorCode) bool {
	var rejected *protocol.Fault
	return errors.As(err, &rejected) && rejected.Code == code
}

// Call sends one command through native.Node.Dispatch and decodes its
// answer, or returns its fault.
func Call[T any](t *testing.T, node *native.Node, command protocol.Command) (T, *protocol.Fault) {
	t.Helper()
	return Request[T](t, node, protocol.NewRequest(node.ID(), command), Wait)
}

func Request[T any](t *testing.T, node *native.Node, request protocol.Request, wait time.Duration) (T, *protocol.Fault) {
	t.Helper()
	var result T
	response := dispatch(t, t.Context(), node, node.ID(), request, wait)
	if response.Fault != nil {
		return result, response.Fault
	}
	// A durable replay returns decoded JSON, so every answer is read as JSON.
	body, err := json.Marshal(response.Output.Data)
	if err == nil && string(body) != "null" {
		err = json.Unmarshal(body, &result)
	}
	if err != nil {
		t.Fatalf("%s output %T: %v", request.Command.Kind, response.Output.Data, err)
	}
	return result, nil
}

// Do is Call for a command that must succeed.
func Do[T any](t *testing.T, node *native.Node, command protocol.Command) T {
	t.Helper()
	result, failure := Call[T](t, node, command)
	if failure != nil {
		t.Fatalf("%s: %s %s", command.Kind, failure.Code, failure.Message)
	}
	return result
}

func RequireFault(t *testing.T, label string, failure *protocol.Fault, code protocol.ErrorCode, message string) {
	t.Helper()
	if failure == nil || failure.Code != code || failure.Message != message {
		t.Fatalf("%s: fault %+v, want %s %q", label, failure, code, message)
	}
}
