import { useEffect, useRef, useCallback } from 'react'
import { useTunnelStore } from '@/store/tunnelStore'
import { useConnectionStore } from '@/store/connectionStore'
import { useAuthStore } from '@/store/authStore'
import { getAuthToken } from '@/lib/auth'
import { wsUrl } from '@/lib/config'
import type { TunnelStatus } from '@/api/types'

interface WebSocketMessage {
  type: string
  payload: {
    tunnelId: string
    status: {
      state: string
      lastError?: string
    }
  }
}

export function useWebSocket() {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const updateTunnel = useTunnelStore((s) => s.updateTunnel)
  const setWsConnected = useConnectionStore((s) => s.setWsConnected)
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const isDemoMode = useTunnelStore((s) => s.isDemoMode)

  const disconnect = useCallback(() => {
    if (reconnectRef.current) {
      clearTimeout(reconnectRef.current)
      reconnectRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.onclose = null
      wsRef.current.close()
      wsRef.current = null
    }
    setWsConnected(false)
  }, [setWsConnected])

  const connect = useCallback(async () => {
    disconnect()

    if (isDemoMode || !isAuthenticated) {
      return
    }

    const token = await getAuthToken()
    if (!token) {
      return
    }

    let url = wsUrl('/ws')
    url += `?token=${encodeURIComponent(token)}`

    const socket = new WebSocket(url)
    wsRef.current = socket

    socket.onopen = () => setWsConnected(true)

    socket.onmessage = (event) => {
      try {
        const message: WebSocketMessage = JSON.parse(event.data)
        if (message.type === 'tunnel_update') {
          const { tunnelId, status } = message.payload
          updateTunnel(tunnelId, {
            status: mapTunnelState(status.state),
            errorMessage: status.lastError || undefined,
          })
        }
      } catch {
        /* ignore malformed frames */
      }
    }

    socket.onclose = () => {
      setWsConnected(false)
      if (wsRef.current === socket) {
        wsRef.current = null
        reconnectRef.current = setTimeout(() => {
          void connect()
        }, 4000)
      }
    }

    socket.onerror = () => setWsConnected(false)
  }, [disconnect, isAuthenticated, isDemoMode, setWsConnected, updateTunnel])

  useEffect(() => {
    void connect()
    return disconnect
  }, [connect, disconnect])

  return { reconnect: connect }
}

function mapTunnelState(state: string): TunnelStatus {
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