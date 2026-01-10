import { create } from 'zustand'
import type { TunnelSpec } from '@/types/tunnel'

interface TunnelStore {
  tunnels: TunnelSpec[]
  selectedTunnelId: string | null
  isDemoMode: boolean

  // Actions
  setTunnels: (tunnels: TunnelSpec[]) => void
  addTunnel: (tunnel: TunnelSpec) => void
  updateTunnel: (id: string, updates: Partial<TunnelSpec>) => void
  removeTunnel: (id: string) => void
  selectTunnel: (id: string | null) => void
  setDemoMode: (enabled: boolean) => void

  // Computed
  getSelectedTunnel: () => TunnelSpec | undefined
  getTunnelsByStatus: (status: TunnelSpec['status']) => TunnelSpec[]
}

export const useTunnelStore = create<TunnelStore>((set, get) => ({
  tunnels: [],
  selectedTunnelId: null,
  isDemoMode: false,

  setTunnels: (tunnels) => set({ tunnels }),

  setDemoMode: (enabled) => set({ isDemoMode: enabled }),

  addTunnel: (tunnel) => set((state) => ({
    tunnels: [...state.tunnels, tunnel]
  })),

  updateTunnel: (id, updates) => set((state) => ({
    tunnels: state.tunnels.map((t) =>
      t.id === id ? { ...t, ...updates } : t
    )
  })),

  removeTunnel: (id) => set((state) => ({
    tunnels: state.tunnels.filter((t) => t.id !== id),
    selectedTunnelId: state.selectedTunnelId === id ? null : state.selectedTunnelId
  })),

  selectTunnel: (id) => set({ selectedTunnelId: id }),

  getSelectedTunnel: () => {
    const state = get()
    return state.tunnels.find((t) => t.id === state.selectedTunnelId)
  },

  getTunnelsByStatus: (status) => {
    return get().tunnels.filter((t) => t.status === status)
  }
}))
