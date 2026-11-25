package nodetest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sailry/sailry-harness/sdk/sdktest"
)

type SentryFixture struct {
	mu     sync.Mutex
	bodies [][]byte
	server *httptest.Server
}

func NewSentry(t *testing.T) *SentryFixture {
	t.Helper()
	fixture := new(SentryFixture)
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		fixture.mu.Lock()
		fixture.bodies = append(fixture.bodies, body)
		fixture.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (fixture *SentryFixture) Options() sdktest.TelemetryOptions {
	dsn := strings.Replace(fixture.server.URL, "http://", "http://public@", 1) + "/42"
	return sdktest.TelemetryOptions{
		Lookup: func(key string) string {
			if key == "SAILRY_SENTRY_DSN" {
				return dsn
			}
			return ""
		},
		Transport: fixture.server.Client().Transport,
	}
}

func (fixture *SentryFixture) Bodies() [][]byte {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return slices.Clone(fixture.bodies)
}
