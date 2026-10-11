package stream

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"honey-forge/internal/contract"
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
	if !contract.ValidHTTPSOrigin(operatorOrigin) {
		return false
	}
	u, _ := url.Parse(operatorOrigin)
	return !e.MatchesHost(u.Host)
}

// Hosts, rather than ports, separate the public resource from the dashboard.
// Reverse proxies must overwrite Host; forwarded host headers are never used.
func (e AgentEndpoint) MatchesHost(host string) bool {
	if !contract.ValidHTTPSOrigin("https://" + host) {
		return false
	}
	u, _ := url.Parse("https://" + host)
	origin, _ := url.Parse(e.origin)
	return strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(origin.Hostname(), "."))
}
