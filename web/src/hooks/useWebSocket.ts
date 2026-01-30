import { useEffect, useRef, useCallback } from 'react'
import { useTunnelStore } from '@/store/tunnelStore'
import type { TunnelSpec } from '@/types/tunnel'

interface WebSocketMessage {
  type: string
  payload: {
    tunnelId: string
    status: {
      state: string
      lastError?: string
      connectedAt?: string
      bytesSent?: number
      bytesReceived?: number
    }
  }
  time: string
}

export function useWebSocket() {
  const ws = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const updateTunnel = useTunnelStore((state) => state.updateTunnel)

  const connect = useCallback(() => {
    // Get the API URL and convert to WebSocket URL
    const apiUrl = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'
    const wsUrl = apiUrl.replace(/^http/, 'ws').replace('/api/v1', '/api/v1/ws')

    try {
      ws.current = new WebSocket(wsUrl)

      ws.current.onopen = () => {
        console.log('🟢 WebSocket connected')
      }

      ws.current.onmessage = (event) => {
        try {
          const message: WebSocketMessage = JSON.parse(event.data)
          
          if (message.type === 'tunnel_update') {
            const { tunnelId, status } = message.payload
            
            // Map the status to our TunnelSpec format
            const updates: Partial<TunnelSpec> = {
              status: mapTunnelState(status.state),
              errorMessage: status.lastError || undefined,
            }

            updateTunnel(tunnelId, updates)
            console.log('📡 Tunnel update received:', tunnelId, status.state)
          }
        } catch (err) {
          console.error('💥 Failed to parse WebSocket message:', err)
        }
      }

      ws.current.onclose = () => {
        console.log('🔴 WebSocket disconnected')
        // Attempt to reconnect after 5 seconds
        reconnectTimeoutRef.current = setTimeout(connect, 5000)
      }

      ws.current.onerror = (error) => {
        console.error('💥 WebSocket error:', error)
      }
    } catch (err) {
      console.error('💥 Failed to create WebSocket connection:', err)
    }
  }, [updateTunnel])

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    
    if (ws.current) {
      ws.current.close()
      ws.current = null
    }
  }, [])

  useEffect(() => {
    connect()
    
    return () => {
      disconnect()
    }
  }, [connect, disconnect])
}

// Helper to map backend tunnel states to frontend status
function mapTunnelState(state: string): TunnelSpec['status'] {
  switch (state) {
    case 'active':
      return 'active'
    case 'pending':
      return 'connecting'
    case 'failed':
      return 'failed'
    case 'stopped':
      return 'disconnected'
    default:
      return 'disconnected'
  }
}
