package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"sort"
	"strconv"
)

type typeKey struct {
	id      string
	version contract.TypeVersion
}

type compiledEntry struct {
	entry           CatalogEntry
	raw             []byte
	config          *configschema.Schema
	events          map[string]*configschema.Schema
	params, results map[string]*configschema.Schema
}

// Service is sealed at startup. Entries, compiled schemas and snapshot never change.
// A new installed type version requires a new definition; old definitions remain included.
type Service struct {
	entries []*compiledEntry
	byType  map[typeKey]*compiledEntry
	etag    string
	cursors *contract.CursorCodec
}

func NewService(definitions []Definition, cursors *contract.CursorCodec) (*Service, error) {
	if cursors == nil {
		return nil, fmt.Errorf("catalog cursor codec is required")
	}
	s := &Service{byType: map[typeKey]*compiledEntry{}, cursors: cursors}
	for _, definition := range definitions {
		e, err := compileEntry(definition)
		if err != nil {
			return nil, fmt.Errorf("compile catalog %s/%d: %w", definition.Entry.TypeID, definition.Entry.TypeVersion, err)
		}
		key := typeKey{string(e.entry.TypeID), e.entry.TypeVersion}
		if s.byType[key] != nil {
			return nil, fmt.Errorf("duplicate catalog type version %s/%d", key.id, key.version)
		}
		s.byType[key] = e
		s.entries = append(s.entries, e)
	}
	sort.Slice(s.entries, func(i, j int) bool {
		a, b := s.entries[i].entry, s.entries[j].entry
		return a.TypeID < b.TypeID || a.TypeID == b.TypeID && a.TypeVersion < b.TypeVersion
	})
	hash := sha256.New()
	for _, e := range s.entries {
		hash.Write(e.raw)
	}
	s.etag = fmt.Sprintf(`"catalog-%x"`, hash.Sum(nil))
	return s, nil
}

func (s *Service) lookup(ctx context.Context, id string, version contract.TypeVersion) (*compiledEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := s.byType[typeKey{id, version}]
	if e == nil {
		return nil, contract.NewError("resource_not_found")
	}
	return e, nil
}

// LookupType is the internal trusted boundary. Operator reads use Read/List for authorization.
func (s *Service) LookupType(ctx context.Context, id string, version contract.TypeVersion) (CatalogEntry, error) {
	e, err := s.lookup(ctx, id, version)
	if err != nil {
		return CatalogEntry{}, err
	}
	return cloneEntry(e)
}

func cloneEntry(e *compiledEntry) (CatalogEntry, error) {
	var entry CatalogEntry
	if err := json.Unmarshal(e.raw, &entry); err != nil {
		return CatalogEntry{}, fmt.Errorf("copy catalog entry: %w", err)
	}
	return entry, nil
}

func (s *Service) Read(ctx context.Context, id string, version contract.TypeVersion) (CatalogEntry, string, error) {
	if err := contract.Authorize(ctx, contract.Admin, contract.Viewer); err != nil {
		return CatalogEntry{}, "", err
	}
	e, err := s.LookupType(ctx, id, version)
	return e, s.etag, err
}

func (s *Service) List(ctx context.Context, q Query) (contract.Page[CatalogEntry], string, error) {
	if err := contract.Authorize(ctx, contract.Admin, contract.Viewer); err != nil {
		return contract.Page[CatalogEntry]{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return contract.Page[CatalogEntry]{}, "", err
	}
	if q.Limit < 1 || q.Limit > 100 || q.TypeID != "" && !contract.ValidTypeID(q.TypeID) {
		return contract.Page[CatalogEntry]{}, "", contract.NewError("invalid_query")
	}
	p, _ := contract.PrincipalFrom(ctx)
	filters := q.TypeID + "|"
	if q.Available != nil {
		filters += strconv.FormatBool(*q.Available)
	}
	scope := contract.CursorScope{OrganizationID: p.OrganizationID, Collection: "trap-types", Filters: filters}
	start := 0
	if q.Cursor != "" {
		position, err := s.cursors.Decode(scope, q.Cursor)
		if err != nil {
			return contract.Page[CatalogEntry]{}, "", err
		}
		n, err := strconv.Atoi(position.After)
		if err != nil || n < 0 || n >= len(s.entries) || position.Boundary != s.etag {
			return contract.Page[CatalogEntry]{}, "", contract.NewError("invalid_cursor")
		}
		start = n + 1
	}
	items := []CatalogEntry{}
	last := -1
	more := false
	for i := start; i < len(s.entries); i++ {
		e := s.entries[i]
		if q.TypeID != "" && string(e.entry.TypeID) != q.TypeID || q.Available != nil && e.entry.AvailableForNewProfiles != *q.Available {
			continue
		}
		if len(items) == q.Limit {
			more = true
			break
		}
		item, err := cloneEntry(e)
		if err != nil {
			return contract.Page[CatalogEntry]{}, "", err
		}
		items = append(items, item)
		last = i
	}
	var next *string
	if more {
		token, err := s.cursors.Encode(scope, contract.CursorPosition{Boundary: s.etag, After: strconv.Itoa(last)})
		if err != nil {
			return contract.Page[CatalogEntry]{}, "", err
		}
		next = &token
	}
	return contract.NewPage(items, next), s.etag, nil
}
