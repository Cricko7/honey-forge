//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"honey-forge/src/backend/modules/auth"
	profilecore "honey-forge/src/backend/modules/profiles"
	profileservice "honey-forge/src/backend/modules/profiles/service"
)

const (
	orgID  = "11111111-1111-4111-8111-111111111111"
	userID = "22222222-2222-4222-8222-222222222222"
)

var admin = auth.AuthContext{UserID: userID, OrganizationID: orgID, Role: auth.RoleAdmin}

func newID() string            { return auth.NewUUID() }
func ptr(value string) *string { return &value }
func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
func request() profilecore.CreateRequest {
	return profilecore.CreateRequest{RequestID: "33333333-3333-4333-8333-333333333333", Name: " Demo ", TypeID: "demo", TypeVersion: 1, Config: profilecore.Object{"visible": "one", "credentials": profilecore.Object{"password": "private"}}}
}
func integrationService(repo *Repository) *profileservice.Service {
	return profileservice.NewService(repo, profilecore.Dependencies{
		LookupType: func(ctx context.Context, id string, version int32) (profilecore.Type, error) {
			if err := ctx.Err(); err != nil {
				return profilecore.Type{}, err
			}
			if id != "demo" || version != 1 {
				return profilecore.Type{}, profilecore.ErrUnknownType
			}
			return profilecore.Type{InteractionLevel: "low", AvailableForNewProfiles: true,
				SecretPaths: func(profilecore.Object) []string { return []string{"/credentials/password"} }, IsSecretPath: func(path string) bool { return path == "/credentials/password" },
				CheckConfig: func(ctx context.Context, value profilecore.Object) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					if _, ok := value["visible"].(string); !ok {
						return profilecore.ErrConfigInvalid
					}
					return nil
				},
			}, nil
		}, HasLiveBindings: CheckLiveBindings,
	})
}
