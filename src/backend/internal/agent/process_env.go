package agent

import "strings"

// Native children receive runtime settings, never the supervisor's credentials.
func childEnvironment(parent []string, sshGateway bool) []string {
	environment := []string{}

	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")

		switch strings.ToUpper(key) {
		case "PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "TZ":
			environment = append(environment, entry)
		case "NM_ROLE", "NM_SANDBOX_IMAGE", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG":
			if sshGateway {
				environment = append(environment, entry)
			}
		}
	}

	return environment
}
