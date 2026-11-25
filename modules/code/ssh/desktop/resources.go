package desktop

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// resourceViews are the terminal and conversation views one template shows.
// They are used on the window's frame only and released with the template;
// releasing a view never closes its terminal or session on the Node.
type resourceViews struct {
	services      view.Services
	terminals     map[protocol.TerminalID]view.Terminal
	conversations map[protocol.SshID]view.Conversation
}

// effect applies a script's terminal view change on the window's frame.
func (v *resourceViews) effect(apply func(protocol.TerminalID)) requestOperation {
	return func(_ context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
		id, err := argument[protocol.TerminalID](arguments, 0)
		if err != nil {
			return nil, err
		}
		if !v.services.Post(func(*ui.Context) { apply(id) }) {
			return nil, sshTransferKey("SSH workspace is closed")
		}
		return json.RawMessage("null"), nil
	}
}

// terminalView mounts only a saved connection's terminal.
func (v *resourceViews) terminalView(id protocol.TerminalID) (view.Terminal, bool) {
	if mounted, ok := v.terminals[id]; ok {
		return mounted, true
	}
	if v.terminals == nil || v.services.MountTerminal == nil {
		return view.Terminal{}, false
	}
	mounted := v.services.MountTerminal(id, func(info view.TerminalInfo) bool { return info.Ssh != nil })
	if mounted.Render == nil {
		return view.Terminal{}, false
	}
	v.terminals[id] = mounted
	return mounted, true
}

func (v *resourceViews) focus(id protocol.TerminalID) {
	if mounted, ok := v.terminalView(id); ok && mounted.Focus != nil {
		mounted.Focus()
	}
}

func (v *resourceViews) releaseTerminal(id protocol.TerminalID) {
	if mounted, ok := v.terminals[id]; ok {
		delete(v.terminals, id)
		if mounted.Close != nil {
			mounted.Close()
		}
	}
}

func (v *resourceViews) terminal(c *ui.Context, props map[string]json.RawMessage, _ bool) (ui.Element, func(*ui.Context), error) {
	var id protocol.TerminalID
	if raw := props["terminal"]; len(raw) == 0 || json.Unmarshal(raw, &id) != nil {
		return ui.Box(c).Size(0, 0), nil, nil
	}
	mounted, ok := v.terminalView(id)
	if !ok {
		return ui.Element{}, nil, sshTransferKey("SSH terminal is unavailable")
	}
	return ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() { mounted.Render(c) }), nil, nil
}

// conversation docks the saved connection's resource sessions. A new draft
// is a fresh SSH resource session; an existing session keeps its saved
// assistant and permission. The heading carries the session controls into
// the Shell's details header.
func (v *resourceViews) conversation(c *ui.Context, props map[string]json.RawMessage, _ bool) (ui.Element, func(*ui.Context), error) {
	var request struct {
		Assistant string `json:"assistant"`
		Resource  struct {
			Kind string         `json:"kind"`
			ID   protocol.SshID `json:"id"`
		} `json:"resource"`
		Heading string `json:"heading"`
	}
	body, err := json.Marshal(props)
	if err != nil {
		return ui.Element{}, nil, err
	}
	if json.Unmarshal(body, &request) != nil || request.Assistant != "ssh" || request.Resource.Kind != "ssh" {
		return ui.Element{}, nil, invalid("invalid SSH conversation")
	}
	profile := request.Resource.ID
	mounted, ok := v.conversations[profile]
	if !ok && v.conversations != nil && v.services.MountConversation != nil {
		mounted = v.services.MountConversation(view.ConversationSpec{
			Match:  func(session view.Session) bool { return SessionMatches(session, profile) },
			Config: func(base view.SessionConfig) view.SessionConfig { return NewSessionConfig(profile, base) },
		})
		if mounted.Render != nil {
			v.conversations[profile] = mounted
		}
	}
	if mounted.Render == nil {
		return ui.Element{}, nil, sshTransferKey("SSH conversation is unavailable")
	}
	element := ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() { mounted.Render(c) })
	var heading func(*ui.Context)
	if mounted.Header != nil {
		heading = func(c *ui.Context) { mounted.Header(c, request.Heading) }
	}
	return element, heading, nil
}

func (v *resourceViews) release() {
	terminals, conversations := v.terminals, v.conversations
	v.terminals, v.conversations = nil, nil
	for _, mounted := range terminals {
		if mounted.Close != nil {
			mounted.Close()
		}
	}
	for _, mounted := range conversations {
		if mounted.Close != nil {
			mounted.Close()
		}
	}
}
