import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from './api'
import type { CreateTunnelRequest } from '@/types/tunnel'
import { useTunnelStore } from '@/store/tunnelStore'

// Query keys
export const tunnelKeys = {
  all: ['tunnels'] as const,
  lists: () => [...tunnelKeys.all, 'list'] as const,
  list: (filters: string) => [...tunnelKeys.lists(), { filters }] as const,
  details: () => [...tunnelKeys.all, 'detail'] as const,
  detail: (id: string) => [...tunnelKeys.details(), id] as const,
  metrics: (id: string) => [...tunnelKeys.all, 'metrics', id] as const,
}

// Queries
export function useTunnels() {
  const setTunnels = useTunnelStore((state) => state.setTunnels)
  const isDemoMode = useTunnelStore((state) => state.isDemoMode)

  const query = useQuery({
    queryKey: tunnelKeys.lists(),
    queryFn: apiClient.getTunnels.bind(apiClient),
    refetchInterval: 5000, // Refetch every 5 seconds for real-time updates
    retry: false, // Don't retry on error (avoid spam when server is down)
    enabled: !isDemoMode, // Don't fetch from API when in demo mode
  })

  // Update store when data changes (but only if NOT in demo mode)
  if (query.data && !isDemoMode) {
    setTunnels(query.data)
  }

  return query
}

export function useTunnel(id: string) {
  return useQuery({
    queryKey: tunnelKeys.detail(id),
    queryFn: () => apiClient.getTunnel(id),
    enabled: !!id,
  })
}

export function useTunnelMetrics(id: string) {
  return useQuery({
    queryKey: tunnelKeys.metrics(id),
    queryFn: () => apiClient.getTunnelMetrics(id),
    enabled: !!id,
    refetchInterval: 2000, // Metrics update more frequently
  })
}

// Mutations
export function useCreateTunnel() {
  const queryClient = useQueryClient()
  const addTunnel = useTunnelStore((state) => state.addTunnel)

  return useMutation({
    mutationFn: (data: CreateTunnelRequest) => apiClient.createTunnel(data),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: tunnelKeys.lists() })
      addTunnel(data)
    },
  })
}

export function useDeleteTunnel() {
  const queryClient = useQueryClient()
  const removeTunnel = useTunnelStore((state) => state.removeTunnel)

  return useMutation({
    mutationFn: (id: string) => apiClient.deleteTunnel(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: tunnelKeys.lists() })
      removeTunnel(id)
    },
  })
}

export function useStartTunnel() {
  const queryClient = useQueryClient()
  const updateTunnel = useTunnelStore((state) => state.updateTunnel)
  const isDemoMode = useTunnelStore((state) => state.isDemoMode)
  const tunnels = useTunnelStore((state) => state.tunnels)

  return useMutation({
    mutationFn: async (id: string) => {
      if (isDemoMode) {
        // In demo mode, just update locally
        const tunnel = tunnels.find(t => t.id === id)
        if (!tunnel) throw new Error('Tunnel not found')
        return {
          ...tunnel,
          status: 'active' as const,
          updatedAt: new Date().toISOString(),
          lastConnected: new Date().toISOString(),
        }
      }
      return apiClient.startTunnel(id)
    },
    onSuccess: (data) => {
      if (!isDemoMode) {
        queryClient.invalidateQueries({ queryKey: tunnelKeys.detail(data.id) })
      }
      updateTunnel(data.id, data)
    },
  })
}

export function useStopTunnel() {
  const queryClient = useQueryClient()
  const updateTunnel = useTunnelStore((state) => state.updateTunnel)
  const isDemoMode = useTunnelStore((state) => state.isDemoMode)
  const tunnels = useTunnelStore((state) => state.tunnels)

  return useMutation({
    mutationFn: async (id: string) => {
      if (isDemoMode) {
        // In demo mode, just update locally
        const tunnel = tunnels.find(t => t.id === id)
        if (!tunnel) throw new Error('Tunnel not found')
        return {
          ...tunnel,
          status: 'stopped' as const,
          updatedAt: new Date().toISOString(),
        }
      }
      return apiClient.stopTunnel(id)
    },
    onSuccess: (data) => {
      if (!isDemoMode) {
        queryClient.invalidateQueries({ queryKey: tunnelKeys.detail(data.id) })
      }
      updateTunnel(data.id, data)
    },
  })
}
