package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"honey-forge/internal/catalog"
	"honey-forge/internal/contract"
	"honey-forge/src/backend/modules/profiles"
	"strings"
)

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
		return profiles.Type{InteractionLevel: string(entry.InteractionLevel), AvailableForNewProfiles: entry.AvailableForNewProfiles,
			CheckConfig: func(ctx context.Context, value profiles.Object) error {
				raw, err := json.Marshal(value)
				if err != nil {
					return fmt.Errorf("encode config: %w", err)
				}
				return profileCatalogError(service.CheckConfig(ctx, id, contract.TypeVersion(version), raw))
			},
			SecretPaths: func(value profiles.Object) []string {
				raw, err := json.Marshal(value)
				if err != nil {
					return nil
				}
				_, paths, err := schema.PublicConfig(raw)
				if err != nil {
					return nil
				}
				return paths
			},
			MergeConfig: func(previous, incoming profiles.Object, clear []string) (profiles.Object, error) {
				old, err := json.Marshal(previous)
				if err != nil {
					return nil, err
				}
				next, err := json.Marshal(incoming)
				if err != nil {
					return nil, err
				}
				raw, err := schema.MergeConfig(old, next, clear)
				if err != nil {
					return nil, profileCatalogError(err)
				}
				var result profiles.Object
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				if err := decoder.Decode(&result); err != nil {
					return nil, err
				}
				return result, nil
			},
		}, nil
	}
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
