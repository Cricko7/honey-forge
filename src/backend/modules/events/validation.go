package events

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin/binding"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/modules/profiles"
)

func parseEvent(raw json.RawMessage, now time.Time) (AgentEvent, error) {
	contract.Configure()
	var e AgentEvent
	invalid := contract.NewError("telemetry_invalid")
	if len(raw) > 16*1024 || contract.CheckJSON(raw) != nil || !httpx.StrictObject(raw, reflect.TypeFor[AgentEvent]()) || json.Unmarshal(raw, &e) != nil {
		return e, invalid
	}
	e.EventID = strings.ToLower(e.EventID)
	e.SessionID = strings.ToLower(e.SessionID)
	if binding.Validator.ValidateStruct(e) != nil {
		return e, invalid
	}
	at, err := time.Parse(time.RFC3339Nano, e.OccurredAt)
	if err != nil || at.After(now.Add(5*time.Minute)) {
		return e, invalid
	}
	ip, err := netip.ParseAddr(e.Source.IP)
	if err != nil || ip.Zone() != "" {
		return e, invalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(e.Data, &object) != nil || object == nil {
		return e, invalid
	}
	e.Source.IP = ip.Unmap().String()
	e.OccurredAt = at.UTC().Format(time.RFC3339Nano)
	return e, nil
}
func checkSnapshot(e AgentEvent, snapshot profiles.Snapshot) error {
	invalid := contract.NewError("telemetry_invalid")
	if e.TypeID != snapshot.TypeID || e.TypeVersion != int64(snapshot.TypeVersion) || e.ProfileRevision != int64(snapshot.ProfileRevision) {
		return invalid
	}
	if e.TypeID != "tcp-banner" {
		if strings.HasPrefix(e.EventType, "service.") {
			return checkServiceSnapshot(e, snapshot)
		}
		return nil
	}
	if e.Destination.Protocol != "tcp" {
		return invalid
	}
	var data struct {
		Name string `json:"listener_name"`
	}
	if json.Unmarshal(e.Data, &data) != nil {
		return invalid
	}
	encoded, err := json.Marshal(snapshot.Config)
	if err != nil {
		return invalid
	}
	var config struct {
		Listeners []struct {
			Name string `json:"name"`
			Port int    `json:"port"`
		} `json:"listeners"`
	}
	if json.Unmarshal(encoded, &config) != nil {
		return invalid
	}
	for _, listener := range config.Listeners {
		if listener.Name == data.Name && listener.Port == e.Destination.Port {
			return nil
		}
	}
	return invalid
}
