export const conpotProtocols = [
  { id: 'modbus', label: 'Modbus TCP', port: 502, transport: 'TCP' },
  { id: 's7comm', label: 'Siemens S7', port: 102, transport: 'TCP' },
  { id: 'snmp', label: 'SNMP', port: 161, transport: 'UDP' },
  { id: 'bacnet', label: 'BACnet/IP', port: 47808, transport: 'UDP' },
  { id: 'enip', label: 'EtherNet/IP', port: 44818, transport: 'TCP' },
] as const

export const dionaeaProtocols = [
  { id: 'smb', label: 'SMB', port: 445, transport: 'TCP' },
  { id: 'mssql', label: 'MSSQL', port: 1433, transport: 'TCP' },
  { id: 'mysql', label: 'MySQL', port: 3306, transport: 'TCP' },
  { id: 'mqtt', label: 'MQTT', port: 1883, transport: 'TCP' },
  { id: 'http', label: 'HTTP', port: 80, transport: 'TCP' },
  { id: 'memcache', label: 'Memcached', port: 11211, transport: 'TCP' },
] as const

export function serviceProtocols(typeId: string) {
  return typeId === 'conpot' ? conpotProtocols : typeId === 'dionaea' ? dionaeaProtocols : []
}
