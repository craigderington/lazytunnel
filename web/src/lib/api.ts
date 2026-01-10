import type { TunnelSpec, CreateTunnelRequest, TunnelMetrics } from '@/types/tunnel'

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

class ApiClient {
  private baseUrl: string

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
  }

  private async request<T>(
    endpoint: string,
    options?: RequestInit
  ): Promise<T> {
    const url = `${this.baseUrl}${endpoint}`

    console.log('🌐 API Request:', {
      method: options?.method || 'GET',
      url,
      body: options?.body ? JSON.parse(options.body as string) : undefined
    })

    try {
      const response = await fetch(url, {
        ...options,
        headers: {
          'Content-Type': 'application/json',
          ...options?.headers,
        },
      })

      console.log('📡 API Response:', {
        status: response.status,
        statusText: response.statusText,
        ok: response.ok
      })

      if (!response.ok) {
        const error = await response.json().catch(() => ({ message: response.statusText }))
        console.error('❌ API Error Response:', error)
        throw new Error(error.message || 'API request failed')
      }

      const data = await response.json()
      console.log('✅ API Success:', data)
      return data
    } catch (error) {
      console.error('💥 API Request Failed:', {
        error,
        message: error instanceof Error ? error.message : 'Unknown error',
        url
      })
      throw error
    }
  }

  // Tunnels
  async getTunnels(): Promise<TunnelSpec[]> {
    return this.request<TunnelSpec[]>('/tunnels')
  }

  async getTunnel(id: string): Promise<TunnelSpec> {
    return this.request<TunnelSpec>(`/tunnels/${id}`)
  }

  async createTunnel(data: CreateTunnelRequest): Promise<TunnelSpec> {
    return this.request<TunnelSpec>('/tunnels', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async deleteTunnel(id: string): Promise<void> {
    return this.request<void>(`/tunnels/${id}`, {
      method: 'DELETE',
    })
  }

  async startTunnel(id: string): Promise<TunnelSpec> {
    return this.request<TunnelSpec>(`/tunnels/${id}/start`, {
      method: 'POST',
    })
  }

  async stopTunnel(id: string): Promise<TunnelSpec> {
    return this.request<TunnelSpec>(`/tunnels/${id}/stop`, {
      method: 'POST',
    })
  }

  async getTunnelMetrics(id: string): Promise<TunnelMetrics> {
    return this.request<TunnelMetrics>(`/tunnels/${id}/metrics`)
  }
}

export const apiClient = new ApiClient(API_BASE_URL)
