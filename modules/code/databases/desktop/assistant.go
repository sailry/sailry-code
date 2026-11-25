package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type assistantView struct {
	services view.Services
	profile  protocol.DatabaseID
	mounted  view.Conversation
	closed   bool

	mu      sync.Mutex
	changed chan struct{}
	cursor  uint64
	body    json.RawMessage
}

func newAssistantView(services view.Services) *assistantView {
	return &assistantView{services: services, changed: make(chan struct{}), body: json.RawMessage("{}")}
}

func (a *assistantView) render(c *ui.Context, props map[string]json.RawMessage, _ bool) (ui.Element, func(*ui.Context), error) {
	var profile struct {
		Kind string              `json:"kind"`
		ID   protocol.DatabaseID `json:"id"`
	}
	var assistant, heading string
	if json.Unmarshal(props["assistant"], &assistant) != nil || assistant != "database" ||
		json.Unmarshal(props["resource"], &profile) != nil || profile.Kind != "database" || profile.ID == (protocol.DatabaseID{}) ||
		json.Unmarshal(props["heading"], &heading) != nil {
		return ui.Element{}, nil, invalid("invalid database assistant resource")
	}
	if a.closed || a.services.MountConversation == nil {
		return ui.Box(c).Size(0, 0), nil, nil
	}
	id := profile.ID
	if a.profile != id {
		a.releaseCurrent()
		a.profile = id
	}
	if a.mounted.Render == nil {
		a.mounted = a.services.MountConversation(view.ConversationSpec{
			Match: func(session view.Session) bool { return matchesDatabaseSession(session, id) },
			Config: func(base view.SessionConfig) view.SessionConfig {
				base.Resource = &protocol.ConnectionResource{Kind: "database", Data: id}
				base.Assistant = nil
				base.Permission = "ask"
				return base
			},
			Tools: &view.ConversationToolObserver{
				Names: []string{"database_query", "database_execute"}, LegacyPackage: "databases",
				Publish: func(history view.ConversationToolHistory) {
					if !a.closed && a.profile == id && history.Session != nil {
						a.publish(id, history)
					}
				},
			},
		})
	}
	mounted := a.mounted
	if mounted.Render == nil {
		return ui.Box(c).Size(0, 0), nil, nil
	}
	element := ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() { mounted.Render(c) })
	var header func(*ui.Context)
	if mounted.Header != nil {
		header = func(c *ui.Context) { mounted.Header(c, heading) }
	}
	return element, header, nil
}

func matchesDatabaseSession(session view.Session, id protocol.DatabaseID) bool {
	resource := session.Config.Resource
	if session.Archived || session.Delegation != nil || resource == nil || resource.Kind != "database" || session.Project != nil {
		return false
	}
	if assistant := session.Config.Assistant; assistant != nil && (assistant.Package.Name != "databases" || assistant.ID != "database" ||
		!view.IsLegacyPackage(assistant.Package.Name, assistant.Package.Digest)) {
		return false
	}
	got, ok := payload[protocol.DatabaseID](resource.Data)
	return ok && got == id
}

func (a *assistantView) release(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.DatabaseID](args, 0)
	if err != nil {
		return nil, err
	}
	if !a.services.Post(func(*ui.Context) {
		if a.profile == id {
			a.releaseCurrent()
		}
	}) {
		return nil, fault("database workspace is closed")
	}
	return null, nil
}

func (a *assistantView) releaseCurrent() {
	mounted := a.mounted
	a.mounted, a.profile = view.Conversation{}, protocol.DatabaseID{}
	if mounted.Close != nil {
		mounted.Close()
	}
}

func (a *assistantView) close() {
	a.closed = true
	a.releaseCurrent()
}

func (a *assistantView) publish(id protocol.DatabaseID, history view.ConversationToolHistory) {
	body, err := json.Marshal(map[string]view.ConversationToolHistory{id.String(): history})
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if bytes.Equal(body, a.body) {
		return
	}
	a.body = body
	a.cursor++
	close(a.changed)
	a.changed = make(chan struct{})
}

func (a *assistantView) next(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	seen, err := argument[string](args, 0)
	if err != nil {
		return nil, err
	}
	for {
		a.mu.Lock()
		cursor, body, changed := strconv.FormatUint(a.cursor, 10), a.body, a.changed
		a.mu.Unlock()
		if seen != cursor {
			return json.Marshal(struct {
				Cursor        string          `json:"cursor"`
				Conversations json.RawMessage `json:"conversations"`
			}{cursor, body})
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}
