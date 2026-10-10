package traps

import (
	"math"
	"strings"
	"time"

	"honey-forge/internal/contract"
)

func nextVersion(current int64) (int64, error) {
	if current >= math.MaxInt32 {
		return 0, contract.NewError("revision_exhausted")
	}
	return current + 1, nil
}
func patch(r *Record, expected int64, req PatchRequest, now time.Time) (bool, error) {
	if r.Revision != expected {
		return false, contract.NewError("revision_mismatch")
	}
	if req.Name == nil && req.Description == nil {
		return false, contract.NewError("validation_failed")
	}
	name, description := r.Name, r.Description
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		description = *req.Description
	}
	if name == r.Name && description == r.Description {
		return false, nil
	}
	revision, err := nextVersion(r.Revision)
	if err != nil {
		return false, err
	}
	version, err := nextVersion(r.StateVersion)
	if err != nil {
		return false, err
	}
	r.Name, r.Description, r.Revision, r.StateVersion, r.UpdatedAt = name, description, revision, version, now
	return true, nil
}
func online(r Record, now time.Time) bool {
	return r.Connectivity == "online" && r.ConnectionID != nil && r.LastSeenAt != nil && !now.After(r.LastSeenAt.Add(30*time.Second))
}
func safeDelete(r Record, expected int64, now time.Time) error {
	if r.Revision != expected {
		return contract.NewError("revision_mismatch")
	}
	if r.ActiveCommandID != nil {
		return contract.NewError("command_in_progress")
	}
	if r.Generation == 0 {
		if r.PendingIngestions > 0 {
			return contract.NewError("trap_buffer_not_empty")
		}
		return nil
	}
	if !online(r, now) {
		return contract.NewError("trap_offline")
	}
	if r.RuntimeState != "stopped" || r.DesiredState != "stopped" {
		return contract.NewError("trap_not_stopped")
	}
	if r.Agent == nil || r.Agent.BufferedEvents != 0 || r.Agent.BufferBytes != 0 || r.Agent.BufferState != "ok" || r.PendingIngestions > 0 {
		return contract.NewError("trap_buffer_not_empty")
	}
	return nil
}

func CredentialsETag(id string, generation int64) string {
	return fmtCredentialsETag(id, generation)
}
