import { apiUrl } from '@/lib/config'
import { clearStoredToken, getAuthToken } from '@/lib/auth'
import type {
  APIError,
  AgentInfo,
  CreateTunnelRequest,
  HealthResponse,
  LoginRequest,
  LoginResponse,
  LogsResponse,
  Tunnel,
  TunnelMetrics,
} from '@/api/types'

export class APIClientError extends Error {
  status: number
  code?: string

  constructor(status: number, message: string, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function parseError(response: Response): Promise<APIClientError> {
  const body = (await response.json().catch(() => ({}))) as APIError
  return new APIClientError(
    response.status,
    body.message || response.statusText,
    body.code
  )
}

export class LazytunnelClient {
  private async request<T>(
    path: string,
    options: RequestInit = {},
    auth = true
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string> | undefined),
    }

    if (auth) {
      const token = await getAuthToken()
      if (token) {
        headers.Authorization = `Bearer ${token}`
      }
    }

    let response = await fetch(apiUrl(path), { ...options, headers })

    if (response.status === 401 && auth) {
      clearStoredToken()
      const token = await getAuthToken()
      if (token) {
        headers.Authorization = `Bearer ${token}`
        response = await fetch(apiUrl(path), { ...options, headers })
      }
    }

    if (!response.ok) {
      throw await parseError(response)
    }

    if (response.status === 204) {
      return undefined as T
    }

    return response.json() as Promise<T>
  }

  getHealth(): Promise<HealthResponse> {
    return this.request<HealthResponse>('/health', {}, false)
  }

  login(body: LoginRequest): Promise<LoginResponse> {
    return this.request<LoginResponse>(
      '/auth/login',
      { method: 'POST', body: JSON.stringify(body) },
      false
    )
  }

  listTunnels(): Promise<Tunnel[]> {
    return this.request<Tunnel[]>('/tunnels')
  }

  getTunnel(id: string): Promise<Tunnel> {
    return this.request<Tunnel>(`/tunnels/${id}`)
  }

  createTunnel(body: CreateTunnelRequest): Promise<Tunnel> {
    return this.request<Tunnel>('/tunnels', {
      method: 'POST',
      body: JSON.stringify(body),
    })
  }

  deleteTunnel(id: string): Promise<void> {
    return this.request<void>(`/tunnels/${id}`, { method: 'DELETE' })
  }

  startTunnel(id: string): Promise<Tunnel> {
    return this.request<Tunnel>(`/tunnels/${id}/start`, { method: 'POST' })
  }

  stopTunnel(id: string): Promise<Tunnel> {
    return this.request<Tunnel>(`/tunnels/${id}/stop`, { method: 'POST' })
  }

  getTunnelMetrics(id: string): Promise<TunnelMetrics> {
    return this.request<TunnelMetrics>(`/tunnels/${id}/metrics`)
  }

  getLogs(lines = 200): Promise<LogsResponse> {
    return this.request<LogsResponse>(`/logs?lines=${lines}`)
  }

  listAgents(): Promise<AgentInfo[]> {
    return this.request<AgentInfo[]>('/agents')
  }
}

export const api = new LazytunnelClient()