import { apiUrl } from '@/lib/config'
import { clearStoredToken, getAuthToken } from '@/lib/auth'
import type {
  APIError,
  AgentInfo,
  CreateTunnelRequest,
  HealthResponse,
  ImportReport,
  LoginRequest,
  LoginResponse,
  LogsResponse,
  Tunnel,
  TunnelMetrics,
} from '@/api/types'

export class APIClientError extends Error {
  status: number
  code?: string
  body?: unknown

  constructor(status: number, message: string, code?: string, body?: unknown) {
    super(message)
    this.status = status
    this.code = code
    this.body = body
  }
}

async function parseError(response: Response): Promise<APIClientError> {
  const body = (await response.json().catch(() => ({}))) as APIError
  return new APIClientError(
    response.status,
    body.message || response.statusText,
    body.code,
    body
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

  updateTunnel(id: string, body: CreateTunnelRequest): Promise<Tunnel> {
    return this.request<Tunnel>(`/tunnels/${id}`, {
      method: 'PUT',
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

  async exportConfig(): Promise<Blob> {
    const token = await getAuthToken()
    const headers: Record<string, string> = {}
    if (token) {
      headers.Authorization = `Bearer ${token}`
    }

    const response = await fetch(apiUrl('/config/export'), { headers })
    if (!response.ok) {
      throw await parseError(response)
    }
    return response.blob()
  }

  importConfig(
    archive: unknown,
    opts: { replace?: boolean; dryRun?: boolean } = {}
  ): Promise<ImportReport> {
    const params = new URLSearchParams({ mode: opts.replace ? 'replace' : 'merge' })
    if (opts.dryRun) {
      params.set('dry_run', 'true')
    }
    return this.request<ImportReport>(`/config/import?${params}`, {
      method: 'POST',
      body: JSON.stringify(archive),
    })
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