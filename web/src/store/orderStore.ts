import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface OrderStore {
  /** Tunnel ids in the user's chosen display order. */
  order: string[]
  setOrder: (ids: string[]) => void
}

/**
 * Holds only the id array. All ordering logic lives in lib/tunnelOrder.ts
 * so it can be tested without mocking storage.
 */
export const useOrderStore = create<OrderStore>()(
  persist(
    (set) => ({
      order: [],
      setOrder: (ids) => set({ order: ids }),
    }),
    {
      name: 'lazytunnel-tunnel-order',
    }
  )
)
