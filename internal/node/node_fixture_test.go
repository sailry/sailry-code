package node

import (
	"context"
	"database/sql"
	"errors"
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

func pluginMessageFault(err error, code protocol.ErrorCode) bool {
	var rejected *protocol.Fault
	return errors.As(err, &rejected) && rejected.Code == code
}
