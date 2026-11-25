package desktop

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func (p *window) register(resources view.Resources) view.Registration {
	requests := &requests{services: p.services, credential: resources.Credential, drafts: make(map[protocol.RequestID]requestDraft)}
	assistant := newAssistantView(p.services)
	p.views = assistant
	return view.Registration{
		Exports: []view.Module{
			{Name: "sailry", Exports: []view.Export{
				{Name: "nextAssistantTools", Async: assistant.next},
				{Name: "releaseAssistant", Sync: assistant.release},
			}},
			{Name: "sailry/sdk", Exports: []view.Export{
				{Name: "nextChange", Async: p.nextChange},
				{Name: "readProjectCatalog", Async: requests.projects},
				{Name: "prepareRequest", Sync: requests.prepareRequest},
				{Name: "completeRequest", Async: requests.complete},
				{Name: "forgetRequest", Sync: requests.forget},
			}},
			{Name: "sailry/connections", Exports: []view.Export{
				{Name: "listDatabases", Async: requests.listDatabases},
				{Name: "listSsh", Async: requests.listSsh},
				{Name: "browseDatabase", Async: requests.browse},
				{Name: "prepareDatabase", Sync: requests.prepareDatabase},
				{Name: "newDatabaseId", Sync: func(context.Context, []json.RawMessage) (json.RawMessage, error) {
					return json.Marshal(protocol.NewDatabaseID().String())
				}},
				{Name: "requestOutcome", Async: requests.outcome},
			}},
		},
		Components: []view.Component{{Module: "sailry", Name: "Conversation", Render: assistant.render}},
		CloseTab:   "db-close-current",
		Release:    assistant.close,
	}
}

type profileWire protocol.DatabaseProfile

type connectionRow struct {
	profileWire
	Revision string `json:"revision"`
}

func (r *requests) listDatabases(ctx context.Context, _ []json.RawMessage) (json.RawMessage, error) {
	data, err := r.read(ctx, protocol.Command{Kind: "list_databases"}, "database_profiles")
	if err != nil {
		return nil, err
	}
	profiles, ok := payload[[]protocol.DatabaseProfile](data)
	if !ok {
		return nil, fault("invalid database profiles")
	}
	rows := make([]connectionRow, len(profiles))
	for i, profile := range profiles {
		rows[i] = connectionRow{profileWire(profile), strconv.FormatUint(profile.Revision, 10)}
	}
	return json.Marshal(rows)
}

func (r *requests) listSsh(ctx context.Context, _ []json.RawMessage) (json.RawMessage, error) {
	data, err := r.read(ctx, protocol.Command{Kind: "list_ssh"}, "ssh_profiles")
	if err != nil {
		return nil, err
	}
	profiles, ok := payload[[]protocol.SshProfile](data)
	if !ok {
		return nil, fault("invalid SSH profiles")
	}
	type choice struct {
		ID   protocol.SshID `json:"id"`
		Name string         `json:"name"`
	}
	rows := make([]choice, len(profiles))
	for i, profile := range profiles {
		rows[i] = choice{profile.ID, profile.Name}
	}
	return json.Marshal(rows)
}

func (r *requests) projects(ctx context.Context, _ []json.RawMessage) (json.RawMessage, error) {
	data, err := r.read(ctx, protocol.Command{Kind: "read_project_catalog"}, "project_catalog")
	if err != nil {
		return nil, err
	}
	return json.Marshal(data)
}

func (r *requests) browse(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.DatabaseID](args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, invalid("database browse requires a profile revision")
	}
	revision, err := revision(args[1])
	if err != nil {
		return nil, err
	}
	var database *string
	if len(args) > 2 {
		database, err = argument[*string](args, 2)
		if err != nil {
			return nil, err
		}
	}
	data, err := r.read(ctx, protocol.Command{Kind: "browse_database", Data: protocol.CommandBrowseDatabasePayload{
		Profile: id, ExpectedRevision: revision, Database: database,
	}}, "database_outcome")
	if err != nil {
		return nil, err
	}
	return json.Marshal(data)
}
