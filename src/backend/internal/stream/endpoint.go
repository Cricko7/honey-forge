package stream

import (
	"fmt"
	"honey-forge/internal/contract"
	"net/url"
	"path"
)

// AgentEndpoint is immutable installation configuration. Profile config cannot
// change its origin/path or turn off TLS because it is never used to build this value.
type AgentEndpoint struct {
	origin string
	path   string
}

func NewAgentEndpoint(origin, streamPath string) (AgentEndpoint, error) {
	u, err := url.Parse(streamPath)
	if !contract.ValidHTTPSOrigin(origin) || err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || u.Path[0] != '/' || path.Clean(u.Path) != u.Path || u.ForceQuery {
		return AgentEndpoint{}, fmt.Errorf("agent endpoint requires an HTTPS origin and a clean absolute path")
	}
	return AgentEndpoint{origin, streamPath}, nil
}
func (e AgentEndpoint) URL() string { return "wss" + e.origin[len("https"):] + e.path }
func (e AgentEndpoint) SeparateFrom(operatorOrigin string) bool {
	return e.origin != operatorOrigin && contract.ValidHTTPSOrigin(operatorOrigin)
}
