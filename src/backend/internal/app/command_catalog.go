package app

import (
	"context"
	"encoding/json"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/commands/service"
)

func CommandActionCheck(catalogService *catalog.Service) service.CheckAction {
	return func(ctx context.Context, typeID string, version int32, action string, params json.RawMessage) error {
		_, err := catalogService.CheckAction(ctx, typeID, contract.TypeVersion(version), action, params)
		return err
	}
}

func CommandResultCheck(catalogService *catalog.Service) service.CheckResult {
	return func(ctx context.Context, typeID string, version int32, action string, result json.RawMessage, hasConfiguration bool) error {
		return catalogService.CheckRuntimeResult(ctx, typeID, contract.TypeVersion(version), action, result, hasConfiguration)
	}
}
