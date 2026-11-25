package node

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

type diagnosticSentryFixture struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	server   *httptest.Server
}

func newDiagnosticSentryFixture(t *testing.T) *diagnosticSentryFixture {
	t.Helper()
	fixture := new(diagnosticSentryFixture)
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, request)
		fixture.bodies = append(fixture.bodies, body)
		fixture.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (fixture *diagnosticSentryFixture) dsn() string {
	return strings.Replace(fixture.server.URL, "http://", "http://public@", 1) + "/42"
}

func (fixture *diagnosticSentryFixture) count() int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return len(fixture.requests)
}

func diagnosticEnvironment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func diagnosticNodeFixture(t *testing.T) *Node {
	t.Helper()
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
	node, err := StartWithOptions(t.Context(), filepath.Join(t.TempDir(), "profile"), Options{
		Assembly: codehost.Assembly(), Environment: &environment, MCP: sdktest.MCPOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return node
}

func diagnosticSessionFixture(t *testing.T, node *Node) protocol.Session {
	t.Helper()
	config := diagnosticSessionConfigFixture(t, node)
	return commandFixture(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}).Data.(protocol.Session)
}

func diagnosticSessionConfigFixture(t *testing.T, node *Node) protocol.SessionConfig {
	t.Helper()
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name = protocol.NewProviderID(), "Fixture"
	provider.Api, provider.Authentication, provider.Enabled = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, true
	provider.Endpoint, provider.DefaultModel = "https://fixture.invalid/v1", "fixture"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 64000, 8192
	vision := false
	model.Capabilities = &protocol.ConversationDiscoveryCapabilities{Vision: &vision}
	provider.Models = []protocol.ConversationModel{model}
	key := protocol.NewSecret("test-only-session-fixture-key")
	response := dispatchFixture(t, node, node.ID(), protocol.NewRequest(node.ID(), protocol.Command{Kind: "save_provider", Data: &protocol.CommandSaveProviderPayload{Provider: provider, Secret: &key}}))
	if response.Fault != nil {
		t.Fatal(response.Fault)
	}
	created := response.Output.Data.(protocol.ConversationProvider)
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Credential = created.ID, created.DefaultModel, created.Credential
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionProject
	return config
}

func diagnosticFault(fault *protocol.Fault) (protocol.NotificationSystemSystemDiagnostic, bool) {
	if fault.Details == nil || fault.Details.Kind != "system_diagnostic" {
		return protocol.NotificationSystemSystemDiagnostic{}, false
	}
	return dispatchPayload[protocol.NotificationSystemSystemDiagnostic](fault.Details.Data)
}
