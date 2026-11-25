package desktop

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type requestOperation = func(context.Context, []json.RawMessage) (json.RawMessage, error)

type requestHost struct {
	read   func(context.Context, protocol.Command) (protocol.Output, error)
	create func(protocol.Command) (string, error)
	secret func(string) (protocol.Secret, error)
	launch func() (any, error)
}

func (h requestHost) prepareRequest(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	return h.prepare(parseWorkspaceCommand(args))
}

func parseWorkspaceCommand(args []json.RawMessage) (protocol.Command, error) {
	value, err := argument[struct {
		Kind string                     `json:"kind"`
		Data map[string]json.RawMessage `json:"data"`
	}](args, 0)
	if err != nil {
		return protocol.Command{}, err
	}
	switch value.Kind {
	case "remove_ssh", "check_ssh", "trust_ssh", "close_ssh_terminal", "inspect_terminal",
		"run_ssh", "transfer_ssh", "modify_ssh_file", "cancel_ssh":
		return decodeCommand(value.Kind, value.Data)
	default:
		return protocol.Command{}, invalid("command is not available in this SSH workspace")
	}
}

func (h requestHost) loadProjects(ctx context.Context, _ []json.RawMessage) (json.RawMessage, error) {
	return h.readOutput(ctx, protocol.Command{Kind: "read_project_catalog"}, "project_catalog")
}

func (h requestHost) listConnections(ctx context.Context, _ []json.RawMessage) (json.RawMessage, error) {
	output, err := h.read(ctx, protocol.Command{Kind: "list_ssh"})
	if err != nil {
		return nil, err
	}
	if output.Kind != "ssh_profiles" {
		return nil, invalid("invalid SSH profile response")
	}
	profiles, ok := payload[[]protocol.SshProfile](output.Data)
	if !ok {
		return nil, invalid("invalid SSH profiles")
	}
	rows := make([]connectionRow, len(profiles))
	for i, profile := range profiles {
		rows[i] = connectionRow{profileWire: profileWire(profile), Revision: strconv.FormatUint(profile.Revision, 10)}
	}
	return json.Marshal(rows)
}

type profileWire protocol.SshProfile

type connectionRow struct {
	profileWire
	Revision string `json:"revision"`
}

func (h requestHost) listTerminals(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.SshID](args, 0)
	if err != nil {
		return nil, err
	}
	command, err := dataCommand("list_ssh_terminals", map[string]any{"profile": id})
	if err != nil {
		return nil, err
	}
	return h.readOutput(ctx, command, "terminals")
}

func (h requestHost) browseDirectory(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	value, err := argument[map[string]json.RawMessage](args, 0)
	if err != nil {
		return nil, err
	}
	command, err := decodeCommand("browse_ssh_directory", value)
	if err != nil {
		return nil, err
	}
	return h.readOutput(ctx, command, "ssh_outcome")
}

func (h requestHost) prepareTerminal(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[protocol.SshID](args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, invalid("SSH terminal requires a profile revision")
	}
	revision, err := scriptRevision(args[1])
	if err != nil {
		return nil, err
	}
	launch, err := h.launch()
	if err != nil {
		return nil, err
	}
	return h.prepare(dataCommand("open_ssh_terminal", map[string]any{"profile": id, "expected_revision": revision, "launch": launch}))
}

type credentialHandle struct {
	Kind       string  `json:"kind"`
	Secret     string  `json:"secret"`
	Passphrase *string `json:"passphrase"`
}

func (h requestHost) prepareConnection(_ context.Context, args []json.RawMessage) (json.RawMessage, error) {
	input, err := argument[struct {
		Profile    map[string]json.RawMessage `json:"profile"`
		Credential *credentialHandle          `json:"credential"`
	}](args, 0)
	if err != nil {
		return nil, err
	}
	revision, err := scriptRevision(input.Profile["revision"])
	if err != nil {
		return nil, err
	}
	input.Profile["revision"], _ = json.Marshal(revision)
	var credential any
	if value := input.Credential; value != nil {
		secret, err := h.secret(value.Secret)
		if err != nil {
			return nil, err
		}
		switch value.Kind {
		case "password":
			credential = map[string]any{"kind": value.Kind, "password": secret}
		case "private_key":
			var passphrase *protocol.Secret
			if value.Passphrase != nil {
				resolved, err := h.secret(*value.Passphrase)
				if err != nil {
					return nil, err
				}
				passphrase = &resolved
			}
			credential = map[string]any{"kind": value.Kind, "key": secret, "passphrase": passphrase}
		default:
			return nil, invalid("invalid SSH credential")
		}
	}
	return h.prepare(dataCommand("save_ssh", map[string]any{"profile": input.Profile, "expected_revision": revision, "credential": credential}))
}

func (h requestHost) prepare(command protocol.Command, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	id, err := h.create(command)
	if err != nil {
		return nil, err
	}
	return json.Marshal(id)
}

func (h requestHost) readOutput(ctx context.Context, command protocol.Command, kind string) (json.RawMessage, error) {
	output, err := h.read(ctx, command)
	if err != nil {
		return nil, err
	}
	if output.Kind != kind {
		return nil, invalid("invalid SSH workspace response")
	}
	return json.Marshal(output.Data)
}

func decodeCommand(kind string, data map[string]json.RawMessage) (protocol.Command, error) {
	if raw, ok := data["expected_revision"]; ok {
		revision, err := scriptRevision(raw)
		if err != nil {
			return protocol.Command{}, err
		}
		data["expected_revision"], _ = json.Marshal(revision)
	}
	return dataCommand(kind, data)
}

func dataCommand(kind string, data any) (protocol.Command, error) {
	envelope := map[string]any{"kind": kind}
	if data != nil {
		envelope["data"] = data
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return protocol.Command{}, err
	}
	var command protocol.Command
	if err := json.Unmarshal(body, &command); err != nil {
		return command, invalid("invalid SSH command")
	}
	return command, nil
}

func scriptRevision(raw json.RawMessage) (uint64, error) {
	var text string
	quoted := len(raw) > 0 && raw[0] == '"'
	if quoted {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, invalid("invalid SSH profile revision")
		}
	} else {
		text = string(raw)
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || strconv.FormatUint(value, 10) != text || !quoted && value > 1<<53-1 {
		return 0, invalid("invalid SSH profile revision")
	}
	return value, nil
}

func argument[T any](args []json.RawMessage, i int) (T, error) {
	var result T
	if i >= len(args) || json.Unmarshal(args[i], &result) != nil {
		return result, invalid("invalid SSH workspace argument")
	}
	return result, nil
}

func payload[T any](value any) (T, bool) {
	if direct, ok := value.(T); ok {
		return direct, true
	}
	if pointer, ok := value.(*T); ok && pointer != nil {
		return *pointer, true
	}
	var zero T
	return zero, false
}

func invalid(message string) *protocol.Fault {
	return &protocol.Fault{Code: protocol.ErrorCodeInvalidRequest, Message: message}
}
