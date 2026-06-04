import { create } from 'zustand'

interface ConnectionState {
  apiHealthy: boolean | null
  wsConnected: boolean
  setApiHealthy: (healthy: boolean) => void
  setWsConnected: (connected: boolean) => void
}

export const useConnectionStore = create<ConnectionState>((set) => ({
  apiHealthy: null,
  wsConnected: false,
  setApiHealthy: (healthy) => set({ apiHealthy: healthy }),
  setWsConnected: (connected) => set({ wsConnected: connected }),
}))