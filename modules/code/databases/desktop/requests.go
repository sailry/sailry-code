package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

var null = json.RawMessage("null")

func invalid(message string) error {
	return &protocol.Fault{Code: protocol.ErrorCodeInvalidRequest, Message: message}
}
func fault(message string) error {
	return &protocol.Fault{Code: protocol.ErrorCodeUnavailable, Message: message}
}

func argument[T any](args []json.RawMessage, index int) (T, error) {
	var value T
	if len(args) <= index || json.Unmarshal(args[index], &value) != nil {
		return value, invalid("invalid database workspace argument")
	}
	return value, nil
}

func revision(raw json.RawMessage) (uint64, error) {
	var text string
	quoted := len(raw) != 0 && raw[0] == '"'
	if quoted {
		if json.Unmarshal(raw, &text) != nil {
			return 0, invalid("invalid database profile revision")
		}
	} else {
		text = string(raw)
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || !quoted && value > (1<<53)-1 {
		return 0, invalid("invalid database profile revision")
	}
	return value, nil
}

type requestDraft struct {
	request protocol.Request
	started bool
}

type requests struct {
	services   view.Services
	credential func(string) (protocol.Secret, error)
	mu         sync.Mutex
	drafts     map[protocol.RequestID]requestDraft
}

func (r *requests) prepare(command protocol.Command) (json.RawMessage, error) {
	request := r.services.Prepare(command)
	if request.ID == (protocol.RequestID{}) {
		return nil, fault("database workspace is closed")
	}
	r.mu.Lock()
	r.drafts[request.ID] = requestDraft{request: request}
	r.mu.Unlock()
	return json.Marshal(request.ID.String())
}

func (r *requests) prepareRequest(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	input, err := argument[struct {
		Kind string                     `json:"kind"`
		Data map[string]json.RawMessage `json:"data"`
	}](args, 0)
	if err != nil {
		return nil, err
	}
	switch input.Kind {
	case "query_database", "check_database", "remove_database":
		value, err := revision(input.Data["expected_revision"])
		if err != nil {
			return nil, err
		}
		input.Data["expected_revision"] = strconv.AppendUint(nil, value, 10)
		body, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		var command protocol.Command
		if json.Unmarshal(body, &command) != nil {
			return nil, invalid("invalid database workspace command")
		}
		return r.prepare(command)
	case "cancel_database":
		var id protocol.RequestID
		if json.Unmarshal(input.Data["request"], &id) != nil {
			return nil, invalid("invalid database cancellation target")
		}
		r.mu.Lock()
		draft, ok := r.drafts[id]
		r.mu.Unlock()
		if !ok || !draft.started || draft.request.Command.Kind != "query_database" && draft.request.Command.Kind != "check_database" {
			return nil, invalid("database operation is not owned by this workspace")
		}
		return r.prepare(protocol.Command{Kind: input.Kind, Data: protocol.CommandCancelDatabasePayload{Request: id}})
	default:
		return nil, invalid("command is not available in this database workspace")
	}
}

func (r *requests) prepareDatabase(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	input, err := argument[struct {
		Profile map[string]json.RawMessage `json:"profile"`
		Secret  string                     `json:"secret"`
		Testing bool                       `json:"testing"`
	}](args, 0)
	if err != nil {
		return nil, err
	}
	revision, err := revision(input.Profile["revision"])
	if err != nil {
		return nil, err
	}
	input.Profile["revision"] = strconv.AppendUint(nil, revision, 10)
	body, err := json.Marshal(input.Profile)
	if err != nil {
		return nil, err
	}
	var profile protocol.DatabaseProfile
	if json.Unmarshal(body, &profile) != nil {
		return nil, invalid("invalid database profile")
	}
	var path string
	switch target := profile.Connection.Data.(type) {
	case *protocol.DatabaseConnectionSqlitePayload:
		path = target.Path
	case *protocol.DatabaseConnectionSocketPayload:
		path = target.Path
	}
	if path == "" {
		profile.Location = nil
	} else if profile.Location == nil || profile.Location.Node == r.services.Node {
		// File-backed drafts name a path on this window's captured Node.
		// An existing path owned by a different Node is never rehomed here.
		profile.Location = &protocol.HostPath{Node: r.services.Node, Path: path}
	}
	var password *protocol.Secret
	if input.Secret != "" {
		secret, err := r.credential(input.Secret)
		if err != nil {
			return nil, err
		}
		password = &secret
	}
	if input.Testing {
		return r.prepare(protocol.Command{Kind: "test_database", Data: protocol.CommandTestDatabasePayload{
			Profile: profile, ExpectedRevision: revision, Password: password,
		}})
	}
	return r.prepare(protocol.Command{Kind: "save_database", Data: protocol.CommandSaveDatabasePayload{
		Profile: profile, ExpectedRevision: revision, Password: password,
	}})
}

func (r *requests) complete(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.RequestID](args, 0)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	draft, ok := r.drafts[id]
	if !ok || draft.started {
		r.mu.Unlock()
		return nil, invalid("database request draft is not dispatchable")
	}
	draft.started = true
	r.drafts[id] = draft
	r.mu.Unlock()
	settle := r.services.Settle
	if settle == nil {
		settle = r.services.Execute
	}
	output, err := settle(ctx, draft.request)
	if err != nil {
		return json.Marshal(map[string]any{"Err": requestFault(err)})
	}
	return publicReceipt(output, nil)
}

func (r *requests) outcome(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.RequestID](args, 0)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	draft, ok := r.drafts[id]
	r.mu.Unlock()
	if !ok || !draft.started || r.services.Resolve == nil {
		return nil, fault("database request outcome is unavailable")
	}
	value, err := r.services.Resolve(ctx, draft.request)
	if err != nil {
		return nil, err
	}
	kind := "admitted"
	var data json.RawMessage
	switch value.State {
	case "applied", "failed":
		kind = "completed"
		data, err = publicReceipt(value.Output, value.Fault)
	case "absent":
		kind = "not_admitted"
	case "unknown", "replayable":
		kind = "unknown"
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data,omitempty"`
	}{kind, data})
}

func (r *requests) forget(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.RequestID](args, 0)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	delete(r.drafts, id)
	r.mu.Unlock()
	return null, nil
}

func requestFault(err error) protocol.Fault {
	var pointer *protocol.Fault
	var value protocol.Fault
	switch {
	case errors.As(err, &pointer) && pointer != nil:
		return *pointer
	case errors.As(err, &value):
		return value
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return protocol.Fault{Code: protocol.ErrorCodeOutcomeUnknown, Message: "database request outcome was not observed"}
	default:
		return protocol.Fault{Code: protocol.ErrorCodeOutcomeUnknown, Message: err.Error()}
	}
}

func publicReceipt(output protocol.Output, failure *protocol.Fault) (json.RawMessage, error) {
	if failure != nil {
		return json.Marshal(map[string]any{"Err": failure})
	}
	body, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	var value json.RawMessage = body
	if output.Kind == "database_outcome" {
		// SQL integers and affected rows must not cross into JavaScript as
		// floats. UseNumber keeps the exact decimals until they are quoted.
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		var wire struct {
			Kind string `json:"kind"`
			Data struct {
				Kind string         `json:"kind"`
				Data map[string]any `json:"data,omitempty"`
			} `json:"data"`
		}
		if err := decoder.Decode(&wire); err != nil {
			return nil, err
		}
		if wire.Data.Kind == "query" {
			result := wire.Data.Data
			if n, ok := result["affected_rows"].(json.Number); ok {
				result["affected_rows"] = n.String()
			}
			rows, _ := result["rows"].([]any)
			for _, row := range rows {
				cells, _ := row.([]any)
				for _, item := range cells {
					cell, _ := item.(map[string]any)
					if cell["kind"] == "integer" {
						if n, ok := cell["value"].(json.Number); ok {
							cell["value"] = n.String()
						}
					}
				}
			}
			value, err = json.Marshal(wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(map[string]json.RawMessage{"Ok": value})
}

func (r *requests) read(ctx context.Context, command protocol.Command, kind string) (any, error) {
	output, err := r.services.Execute(ctx, r.services.Prepare(command))
	if err != nil {
		return nil, err
	}
	if output.Kind != kind {
		return nil, fmt.Errorf("unexpected database workspace response %q for %s", output.Kind, command.Kind)
	}
	return output.Data, nil
}

func payload[T any](value any) (T, bool) {
	if value, ok := value.(T); ok {
		return value, true
	}
	if value, ok := value.(*T); ok && value != nil {
		return *value, true
	}
	var zero T
	return zero, false
}
