package traps

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
)

func (s *Service) List(ctx context.Context, q ListQuery) ([]Trap, bool, error) {
	org, err := access(ctx, false)
	if err != nil {
		return nil, false, err
	}
	if q.Limit < 1 || q.Limit > 100 || q.ProfileID != "" && !contract.ValidID(q.ProfileID) || q.Connectivity != "" && q.Connectivity != "online" && q.Connectivity != "offline" {
		return nil, false, contract.NewError("invalid_query")
	}
	if s.store == nil {
		return nil, false, contract.NewError("service_unavailable")
	}
	return s.store.List(ctx, org, q)
}
func (h *Handler) list(c *gin.Context) {
	if !httpx.EmptyBody(c) {
		return
	}
	values, ok := contract.RequestQuery(c, "limit", "cursor", "profile_id", "connectivity")
	if !ok {
		return
	}
	parsed, err := contract.ParseListQuery(values, "profile_id", "connectivity")
	if err != nil {
		contract.Fail(c, err)
		return
	}
	q := ListQuery{Limit: parsed.Limit, ProfileID: strings.ToLower(values.Get("profile_id")), Connectivity: values.Get("connectivity"), Boundary: time.Now().UTC(), BoundaryID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
	if value, present := values["profile_id"]; present && !contract.ValidID(value[0]) {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}
	if value, present := values["connectivity"]; present && value[0] != "online" && value[0] != "offline" {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}
	principal, _ := contract.PrincipalFrom(c.Request.Context())
	scope := contract.CursorScope{OrganizationID: principal.OrganizationID, Collection: "traps", Filters: q.ProfileID + ":" + q.Connectivity}
	if parsed.Cursor != "" {
		if h.cursors == nil {
			h.fail(c, contract.NewError("service_unavailable"))
			return
		}
		position, err := h.cursors.Decode(scope, parsed.Cursor)
		var valid bool
		if err == nil {
			q.Boundary, q.BoundaryID, valid = parsePosition(position.Boundary)
			if valid {
				q.AfterTime, q.AfterID, valid = parsePosition(position.After)
			}
		}
		if err != nil || !valid {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
	}
	items, more, listErr := h.service.List(c.Request.Context(), q)
	if listErr != nil {
		h.fail(c, listErr)
		return
	}
	var next *string
	if more {
		if h.cursors == nil {
			h.fail(c, contract.NewError("service_unavailable"))
			return
		}
		last := items[len(items)-1]
		cursor, err := h.cursors.Encode(scope, contract.CursorPosition{Boundary: q.Boundary.Format(time.RFC3339Nano) + "/" + q.BoundaryID, After: last.CreatedAt.Format(time.RFC3339Nano) + "/" + last.ID})
		if err != nil {
			h.fail(c, err)
			return
		}
		next = &cursor
	}
	c.JSON(200, contract.NewPage(items, next))
}
func parsePosition(value string) (time.Time, string, bool) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 || !contract.ValidID(parts[1]) {
		return time.Time{}, "", false
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	return at, parts[1], err == nil
}
