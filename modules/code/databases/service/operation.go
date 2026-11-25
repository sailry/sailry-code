package databases

import (
	"context"

	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func ExecuteOperation(ctx context.Context, prepared node.DatabaseOperation) (protocol.DatabaseOutcome, error) {
	operation := Operation{Profile: prepared.Profile, Password: prepared.Password, Storage: prepared.Storage}
	if prepared.Ssh != nil {
		operation.SSH = &SSH{Profile: prepared.Ssh.Profile, Credential: prepared.Ssh.Credential}
	}
	if prepared.Query != nil {
		operation.Query = &Query{SQL: prepared.Query.Sql, RowLimit: uint32(prepared.Query.RowLimit), TimeoutMS: prepared.Query.TimeoutMs}
	}
	if prepared.Browse != nil {
		operation.Browse = &Browse{Database: prepared.Browse.Database}
	}
	return Execute(ctx, operation)
}
