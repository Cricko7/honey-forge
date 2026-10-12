package redistrap

import (
	"fmt"
	"strings"
	"time"
)

func (d *database) info(sections []string) string {
	wanted := map[string]bool{}
	for _, section := range sections {
		wanted[strings.ToLower(section)] = true
	}

	all := len(sections) == 0 || wanted["default"] || wanted["all"] || wanted["everything"]
	var result strings.Builder
	if all || wanted["server"] {
		fmt.Fprintf(&result, "# Server\r\nredis_version:7.2.0\r\nredis_git_sha1:00000000\r\nredis_git_dirty:0\r\nredis_mode:standalone\r\nos:Linux 6.1.0 x86_64\r\narch_bits:64\r\nmultiplexing_api:epoll\r\nrun_id:%s\r\ntcp_port:%d\r\nuptime_in_seconds:%d\r\n\r\n", d.identity.runID, d.identity.port, int64(time.Since(d.identity.started).Seconds()))
	}

	if all || wanted["replication"] {
		result.WriteString("# Replication\r\nrole:master\r\nconnected_slaves:0\r\nmaster_repl_offset:0\r\n\r\n")
	}

	if all || wanted["keyspace"] {
		result.WriteString("# Keyspace\r\n")
		if len(d.values) > 0 {
			fmt.Fprintf(&result, "db0:keys=%d,expires=0,avg_ttl=0\r\n", len(d.values))
		}

		result.WriteString("\r\n")
	}

	return result.String()
}
