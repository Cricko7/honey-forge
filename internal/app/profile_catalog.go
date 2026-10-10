package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"honey-forge/src/backend/modules/catalog"
	"honey-forge/src/backend/modules/profiles"
)

// ProfileTypeLookup adapts immutable catalog versions to the profile service.
func ProfileTypeLookup(service *catalog.Service) func(context.Context, string, int32) (profiles.Type, error) {
	return func(ctx context.Context, id string, version int32) (profiles.Type, error) {
		entry, err := service.LookupType(ctx, id, contract.TypeVersion(version))
		if err != nil {
			return profiles.Type{}, profileCatalogError(err)
		}
		schema, err := service.ConfigSchema(ctx, id, contract.TypeVersion(version))
		if err != nil {
			return profiles.Type{}, profileCatalogError(err)
		}
		return profiles.Type{
			InteractionLevel:        string(entry.InteractionLevel),
			AvailableForNewProfiles: entry.AvailableForNewProfiles,
			CheckConfig: func(ctx context.Context, value profiles.Object) error {
				return checkCatalogConfig(ctx, service, id, version, value)
			},
			SecretPaths: func(value profiles.Object) []string {
				return catalogSecretPaths(schema, value)
			},
			MergeConfig: func(previous, incoming profiles.Object, clear []string) (profiles.Object, error) {
				return mergeCatalogConfig(schema, previous, incoming, clear)
			},
		}, nil
	}
}

func checkCatalogConfig(
	ctx context.Context,
	service *catalog.Service,
	id string,
	version int32,
	value profiles.Object,
) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return profileCatalogError(service.CheckConfig(ctx, id, contract.TypeVersion(version), raw))
}

func catalogSecretPaths(schema *configschema.Schema, value profiles.Object) []string {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	_, paths, err := schema.PublicConfig(raw)
	if err != nil {
		return nil
	}
	return paths
}

func mergeCatalogConfig(
	schema *configschema.Schema,
	previous, incoming profiles.Object,
	clear []string,
) (profiles.Object, error) {
	old, err := json.Marshal(previous)
	if err != nil {
		return nil, fmt.Errorf("encode stored config: %w", err)
	}
	next, err := json.Marshal(incoming)
	if err != nil {
		return nil, fmt.Errorf("encode incoming config: %w", err)
	}
	raw, err := schema.MergeConfig(old, next, clear)
	if err != nil {
		return nil, profileCatalogError(err)
	}

	var result profiles.Object
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode merged config: %w", err)
	}
	return result, nil
}

func profileCatalogError(err error) error {
	if err == nil {
		return nil
	}
	var source *contract.Error
	if !errors.As(err, &source) {
		return err
	}
	var cause error
	switch source.Code {
	case "resource_not_found":
		cause = profiles.ErrUnknownType
	case "config_too_large":
		cause = profiles.ErrConfigTooLarge
	case "validation_failed":
		cause = profiles.ErrValidation
		for _, field := range source.Fields {
			if field.Path == "/config" || strings.HasPrefix(field.Path, "/config/") {
				cause = profiles.ErrConfigInvalid
				break
			}
		}
	default:
		cause = profiles.ErrConfigInvalid
	}
	result := &profiles.ValidationError{Cause: cause}
	for _, field := range source.Fields {
		result.Fields = append(result.Fields, profiles.FieldError{Path: field.Path, Code: field.Code, Message: field.Message})
	}
	return result
}
