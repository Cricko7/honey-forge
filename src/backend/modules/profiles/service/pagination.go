package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	profilecore "honey-forge/modules/profiles"
	"strings"
	"time"

	"honey-forge/modules/auth"
)

type cursor struct {
	OrganizationID, TypeID string
	Boundary               int64
	AfterTime              time.Time
	AfterID                string
}

func (s *Service) List(ctx context.Context, a auth.AuthContext, opts profilecore.ListOptions) (profilecore.Page, error) {
	if err := Authorize(a, false); err != nil {
		return profilecore.Page{}, err
	}

	if opts.Limit == 0 {
		opts.Limit = 50
	}

	if opts.Limit < 1 || opts.Limit > 100 || opts.TypeID != "" && !profilecore.ValidTypeID(opts.TypeID) {
		return profilecore.Page{}, profilecore.ErrValidation
	}

	q := profilecore.ListQuery{Limit: opts.Limit, TypeID: opts.TypeID}
	if opts.Cursor != "" {
		c, err := s.decodeCursor(opts.Cursor)
		if err != nil || c.OrganizationID != a.OrganizationID || c.TypeID != opts.TypeID {
			return profilecore.Page{}, profilecore.ErrInvalidCursor
		}

		q.Boundary, q.AfterTime, q.AfterID = c.Boundary, c.AfterTime, c.AfterID
	}

	if s.store == nil {
		return profilecore.Page{}, profilecore.ErrDatabaseUnavailable
	}

	items, boundary, err := s.store.List(ctx, a.OrganizationID, q)
	if err != nil {
		return profilecore.Page{}, err
	}

	page := profilecore.Page{Items: make([]profilecore.Profile, 0, min(len(items), q.Limit))}
	for _, p := range items[:min(len(items), q.Limit)] {
		page.Items = append(page.Items, profilecore.Redacted(p))
	}

	if len(items) > q.Limit {
		last := items[q.Limit-1]
		token, err := s.encodeCursor(cursor{a.OrganizationID, opts.TypeID, boundary, last.CreatedAt, last.ID})
		if err != nil {
			return profilecore.Page{}, err
		}

		page.NextCursor = &token
	}

	return page, nil
}

func (s *Service) encodeCursor(c cursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, s.cursorKey[:])
	if _, err := mac.Write(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) decodeCursor(token string) (cursor, error) {
	var c cursor
	if len(token) > 2048 {
		return c, profilecore.ErrInvalidCursor
	}

	data, sig, ok := strings.Cut(token, ".")
	if !ok {
		return c, profilecore.ErrInvalidCursor
	}

	raw, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return c, profilecore.ErrInvalidCursor
	}

	signature, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return c, profilecore.ErrInvalidCursor
	}

	mac := hmac.New(sha256.New, s.cursorKey[:])
	if _, err := mac.Write(raw); err != nil {
		return c, err
	}

	if !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(raw, &c) != nil ||
		c.Boundary < 1 || !profilecore.ValidID(c.AfterID) || c.AfterTime.IsZero() {
		return c, profilecore.ErrInvalidCursor
	}

	return c, nil
}
