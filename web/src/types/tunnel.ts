export type TunnelType = 'local' | 'remote' | 'dynamic'
export type TunnelStatus = 'active' | 'connecting' | 'disconnected' | 'failed' | 'stopped'

export interface Hop {
  host: string
  port: number
  user: string
  auth_method: 'key' | 'password' | 'agent' | 'cert'  // snake_case to match backend API
  key_id?: string  // snake_case to match backend API
}

export interface TunnelSpec {
  id: string
  name: string
  owner: string
  type: TunnelType
  hops: Hop[]
  localPort: number
  remoteHost: string
  remotePort: number
  autoReconnect: boolean
  keepAlive: number
  maxRetries: number
  status: TunnelStatus
  createdAt: string
  updatedAt: string
  lastConnected?: string
  errorMessage?: string
}

export interface CreateTunnelRequest {
  name: string
  type: TunnelType
  hops: Hop[]
  localPort: number
  remoteHost: string
  remotePort: number
  autoReconnect?: boolean
  keepAlive?: number
  maxRetries?: number
}

export interface TunnelMetrics {
  tunnelId: string
  bytesIn: number
  bytesOut: number
  connectionsActive: number
  uptime: number
  lastHeartbeat: string
}
