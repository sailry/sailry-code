package desktop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// register supplies the template's Code-owned operations. Every request is
// prepared by the window's services for its local Node; the template only
// names commands from the SSH workspace's own list.
func (p *window) register(resources view.Resources) view.Registration {
	s := p.services
	drafts := &requestDrafts{services: s, requests: map[string]protocol.Request{}}
	views := &resourceViews{services: s, terminals: map[protocol.TerminalID]view.Terminal{}, conversations: map[protocol.SshID]view.Conversation{}}
	host := requestHost{
		read: func(ctx context.Context, command protocol.Command) (protocol.Output, error) {
			return s.Execute(ctx, s.Prepare(command))
		},
		create: drafts.create, secret: resources.Credential, launch: resources.Launch,
	}
	transfers := transferViews{
		store: p.transfers, host: newTransferHost(p.ctx, s),
		available:   func(_ protocol.SshProfile, live bool) bool { return !live || s.Connected() },
		chooseFiles: s.PickFiles, chooseFolder: s.PickFolder, chooseSave: s.PickDestination,
		droppedFiles: resources.DroppedFiles, preview: s.Preview,
	}
	call := func(name string, operation requestOperation) view.Export {
		return view.Export{Name: name, Sync: operation}
	}
	await := func(name string, operation requestOperation) view.Export {
		return view.Export{Name: name, Async: operation}
	}
	return view.Registration{
		Exports: []view.Module{
			{Name: "sailry", Exports: []view.Export{
				call("focusTerminal", views.effect(views.focus)),
				call("releaseTerminal", views.effect(views.releaseTerminal)),
				call("releaseConversation", views.effectSsh(views.releaseConversation)),
			}},
			{Name: "sailry/sdk", Exports: []view.Export{
				await("nextChange", p.nextChange),
				await("readProjectCatalog", host.loadProjects),
				call("prepareRequest", host.prepareRequest),
				await("completeRequest", drafts.complete),
				call("forgetRequest", drafts.forget),
				call("faultCode", faultCodeOperation),
			}},
			{Name: "sailry/connections", Exports: []view.Export{
				await("listSsh", host.listConnections),
				call("newSshId", func(context.Context, []json.RawMessage) (json.RawMessage, error) {
					return json.Marshal(protocol.NewSshID().String())
				}),
				call("prepareSsh", host.prepareConnection),
				await("listSshTerminals", host.listTerminals),
				call("prepareSshTerminal", host.prepareTerminal),
				await("browseSshDirectory", host.browseDirectory),
				await("requestOutcome", drafts.outcome),
			}},
			{Name: "sailry/ssh-transfers", Exports: []view.Export{
				call("uploadDropped", func(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
					return transfers.uploadFiles(ctx, arguments, true)
				}),
				await("selectUpload", func(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
					return transfers.uploadFiles(ctx, arguments, false)
				}),
				await("selectDownload", transfers.selectDownload),
				await("nextTransfers", transfers.nextTransfers),
				call("transferAction", transfers.transferAction),
				call("previewFile", transfers.previewFile),
			}},
		},
		Components: []view.Component{
			{Module: "sailry", Name: "Terminal", Render: views.terminal},
			{Module: "sailry", Name: "Conversation", Render: views.conversation},
		},
		CloseTab: "ssh-close-current",
		Release:  views.release,
	}
}

func (p *window) nextChange(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	seen := ""
	if len(arguments) != 0 && string(arguments[0]) != "null" {
		value, err := argument[string](arguments, 0)
		if err != nil {
			return nil, err
		}
		seen = value
	}
	if p.services.Next == nil {
		return nil, sshTransferKey("change observation is unavailable")
	}
	change, err := p.services.Next(ctx, seen)
	if err != nil {
		return nil, err
	}
	return json.Marshal(change)
}

// requestDrafts are the template's prepared requests. Each keeps its exact
// identity until the template forgets it, so checking an uncertain outcome
// never dispatches the command again.
type requestDrafts struct {
	services view.Services
	mu       sync.Mutex
	requests map[string]protocol.Request
}

func (d *requestDrafts) create(command protocol.Command) (string, error) {
	request := d.services.Prepare(command)
	id := request.ID.String()
	d.mu.Lock()
	d.requests[id] = request
	d.mu.Unlock()
	return id, nil
}

func (d *requestDrafts) get(arguments []json.RawMessage) (protocol.Request, error) {
	id, err := argument[string](arguments, 0)
	if err != nil {
		return protocol.Request{}, err
	}
	d.mu.Lock()
	request, ok := d.requests[id]
	d.mu.Unlock()
	if !ok {
		return protocol.Request{}, invalid("SSH request draft is unavailable")
	}
	return request, nil
}

func (d *requestDrafts) complete(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	request, err := d.get(arguments)
	if err != nil {
		return nil, err
	}
	settle := d.services.Settle
	if settle == nil {
		settle = d.services.Execute
	}
	output, err := settle(ctx, request)
	if err != nil {
		return json.Marshal(map[string]any{"Err": sshTransferFault(err)})
	}
	return json.Marshal(map[string]any{"Ok": output})
}

func (d *requestDrafts) outcome(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	request, err := d.get(arguments)
	if err != nil {
		return nil, err
	}
	if d.services.Outcome == nil {
		return nil, sshTransferKey("request outcomes are unavailable")
	}
	outcome, err := d.services.Outcome(ctx, request)
	if err != nil {
		return nil, err
	}
	return json.Marshal(outcome)
}

func (d *requestDrafts) forget(_ context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[string](arguments, 0)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	delete(d.requests, id)
	d.mu.Unlock()
	return json.RawMessage("null"), nil
}

// faultCodeOperation reads the code of a failure a script caught from this
// module, which carries the Node fault after the operation's name.
func faultCodeOperation(_ context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	message, err := argument[string](arguments, 0)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(message, "`sailry/") {
		if _, code, found := strings.Cut(message, "`: "); found {
			message = code
		}
	}
	var fault protocol.Fault
	switch {
	case json.Unmarshal([]byte(message), &fault) == nil && fault.Code != "":
		return json.Marshal(string(fault.Code))
	case message == "unconfirmed":
		return json.Marshal(string(protocol.ErrorCodeOutcomeUnknown))
	}
	return json.Marshal("failed")
}
