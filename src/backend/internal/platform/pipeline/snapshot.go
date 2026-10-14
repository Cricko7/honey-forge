package pipeline

import (
	"encoding/json"

	"honey-forge/modules/profiles"
)

type flowConfig struct {
	Services []struct{ Name, Entry string } `json:"services"`
	Blocks   []struct {
		ID, Kind string
		Config   struct {
			TargetID              string `json:"target_id"`
			Service, Method, Path string
		}
	} `json:"blocks"`
}

func config(snapshot profiles.Snapshot) (flowConfig, bool) {
	var value flowConfig
	raw, err := json.Marshal(snapshot.Config)
	ok := err == nil && snapshot.TypeID == "custom-http" && json.Unmarshal(raw, &value) == nil
	return value, ok
}

func AllowsSend(snapshot profiles.Snapshot, r Request) bool {
	c, ok := config(snapshot)
	if !ok {
		return false
	}
	for _, b := range c.Blocks {
		if b.ID == r.NodeID && b.Kind == "send" && b.Config.TargetID == r.TargetID && b.Config.Service == r.Service && b.Config.Method == r.Method && b.Config.Path == r.Path {
			return true
		}
	}
	return false
}

func AllowsReceive(snapshot profiles.Snapshot, r Request) bool {
	c, ok := config(snapshot)
	if !ok {
		return false
	}
	for _, service := range c.Services {
		if service.Name != r.Service {
			continue
		}
		for _, b := range c.Blocks {
			if b.ID == service.Entry && b.Kind == "input" && (b.Config.Method == "ANY" || b.Config.Method == r.Method) && (b.Config.Path == "*" || b.Config.Path == r.Path) {
				return true
			}
		}
	}
	return false
}
