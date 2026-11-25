package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sailry/sailry-harness/sdk/native/link"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

type Node = native.Node

type Options = native.Options

var StartWithOptions = native.StartWithOptions

func filesAssembly() native.Assembly {
	assembly := native.HarnessAssembly()
	assembly.BundledDefaults = []native.DefaultPlugin{}
	assembly.Modules = slices.DeleteFunc(assembly.Modules, func(module native.Module) bool { return module.ID() != "files" })
	return assembly
}

func dispatchFixture(t *testing.T, node *Node, caller protocol.NodeID, request protocol.Request) link.Response {
	t.Helper()
	accepted, err := node.Dispatch(context.Background(), caller, request)
	if err != nil {
		var failure *protocol.Fault
		if errors.As(err, &failure) {
			return link.Response{Fault: failure}
		}
		t.Fatal(err)
	}
	value, ok := <-accepted.Completion
	if !ok {
		t.Fatal("completion was lost")
	}
	return value
}

func commandFixture(t *testing.T, node *Node, command protocol.Command) protocol.Output {
	t.Helper()
	value := dispatchFixture(t, node, node.ID(), protocol.NewRequest(node.ID(), command))
	if value.Fault != nil {
		t.Fatal(value.Fault)
	}
	return value.Output
}

func decodeCommandFixture(t *testing.T, kind string, data any) protocol.Command {
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

func snapshotFixture(t *testing.T, node *Node) protocol.Snapshot {
	t.Helper()
	value, err := node.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func dispatchPayload[Value any](input any) (Value, bool) {
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

func trustedCallerFixture(t *testing.T, node *Node, caller protocol.NodeID) {
	t.Helper()
	if err := sdktest.Inspect(node).Store().Node.Write(t.Context(), func(db *sql.Conn) error {
		return sdktest.SetLinkTrust(t.Context(), db, caller, true)
	}); err != nil {
		t.Fatal(err)
	}
}

func installPluginFixture(t *testing.T, node *Node, name string, resources map[string]string) protocol.PluginInfo {
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

func pluginMessageFault(err error, code protocol.ErrorCode) bool {
	var rejected *protocol.Fault
	return errors.As(err, &rejected) && rejected.Code == code
}
