package app

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/stream"
)

func configureAgentEndpoint(agentURL, publicOrigin string, browserOrigins []string) (stream.AgentEndpoint, error) {
	u, err := url.Parse(agentURL)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != stream.AgentPath || u.RawPath != "" {
		return stream.AgentEndpoint{}, fmt.Errorf("AGENT_WS_URL must be explicitly set to wss://host/assets/stream")
	}

	endpoint, err := stream.NewAgentEndpoint("https://"+u.Host, stream.AgentPath)
	if err != nil {
		return stream.AgentEndpoint{}, fmt.Errorf("configure agent endpoint: %w", err)
	}

	if !endpoint.SeparateFrom(publicOrigin) {
		return stream.AgentEndpoint{}, fmt.Errorf("API_PUBLIC_ORIGIN must be HTTPS and use a different host from AGENT_WS_URL")
	}

	for _, origin := range browserOrigins {
		if !endpoint.SeparateFrom(origin) {
			return stream.AgentEndpoint{}, fmt.Errorf("AGENT_WS_URL must use a different host from every BROWSER_ORIGINS entry")
		}
	}

	return endpoint, nil
}

// Run before CORS and the operator contract so the resource host cannot disclose
// dashboard routes or their JSON errors, even if the reverse proxy is misrouted.
func telemetryHost(endpoint stream.AgentEndpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		isResourceHost := endpoint.MatchesHost(c.Request.Host)
		if isResourceHost {
			if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
				switch c.Request.URL.Path {
				case "/":
					stream.ResourceIndex(c)
					return
				case stream.AgentPath:
					c.Next()
					return
				}
			}
		} else if c.Request.URL.Path != stream.AgentPath {
			c.Next()
			return
		}

		c.AbortWithStatus(http.StatusNotFound)
		if c.Request.Method != http.MethodHead {
			c.Data(http.StatusNotFound, "text/plain; charset=utf-8", []byte("Not Found\n"))
		}
	}
}
