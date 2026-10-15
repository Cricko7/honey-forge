package catalog

// ConpotTransport fixes the wire transport for each installed Conpot protocol.
// Profiles cannot select arbitrary listeners or change this boundary.
func ConpotTransport(protocol string) string {
	switch protocol {
	case "modbus", "s7comm", "enip":
		return "tcp"
	case "snmp", "bacnet":
		return "udp"
	default:
		return ""
	}
}
