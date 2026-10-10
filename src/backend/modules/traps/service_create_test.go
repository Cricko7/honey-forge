package traps

import (
	"context"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type creationStore struct {
	store
	called bool
}

func (f *creationStore) Create(ctx context.Context, org string, req CreateRequest, check func(context.Context, profiles.Profile) error) (CreateResult, error) {
	f.called = true
	return CreateResult{}, check(ctx, profiles.Profile{ID: req.ProfileID, TypeID: "tcp-banner", TypeVersion: 1})
}

type unavailableCatalog struct{}

func (unavailableCatalog) LookupType(context.Context, string, contract.TypeVersion) (catalog.CatalogEntry, error) {
	return catalog.CatalogEntry{AvailableForNewProfiles: false}, nil
}
func TestCreateRejectsRetiredType(t *testing.T) {
	storage := &creationStore{}
	service := NewService(storage, unavailableCatalog{}, "wss://center.example/assets/stream", nil)
	ctx := contract.WithPrincipal(t.Context(), contract.Principal{UserID: contract.NewID(), OrganizationID: contract.NewID(), Role: contract.Admin})
	_, err := service.Create(ctx, CreateRequest{RequestID: string(contract.NewID()), ProfileID: string(contract.NewID()), Name: "Demo"})
	requireCode(t, err, "trap_type_unavailable")
	if !storage.called {
		t.Fatal("binding not checked in creation transaction")
	}
}
