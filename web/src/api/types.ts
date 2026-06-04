/** API types aligned with api/openapi.yaml */

export type TunnelType = 'local' | 'remote' | 'dynamic'
export type TunnelStatus = 'active' | 'connecting' | 'disconnected' | 'failed' | 'stopped'

export interface Hop {
  host: string
  port: number
  user: string
  auth_method: 'key' | 'password' | 'agent' | 'cert'
  key_id?: string
}

export interface Tunnel {
  id: string
  name: string
  owner: string
  agentId?: string
  desiredStatus?: string
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

export interface HealthResponse {
  status: string
  time: string
  version?: string
  tunnels?: {
    total: number
    active: number
    failed: number
  }
}

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  tokenType: string
  expiresIn: number
}

export interface APIError {
  code?: string
  message?: string
  timestamp?: string
}

export interface LogsResponse {
  logs: Array<Record<string, unknown>>
}

export interface AgentInfo {
  id: string
  hostname: string
  version: string
  status: string
  last_seen: string
  tunnel_count?: number
}