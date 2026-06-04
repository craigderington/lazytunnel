import { create } from 'zustand'
import {
  clearStoredToken,
  getStoredToken,
  loginWithCredentials,
  probeAuthRequired,
} from '@/lib/auth'

interface AuthState {
  token: string | null
  isReady: boolean
  authRequired: boolean
  isAuthenticated: boolean
  error: string | null
  initialize: () => Promise<void>
  login: (username: string, password: string) => Promise<void>
  logout: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  token: getStoredToken(),
  isReady: false,
  authRequired: false,
  isAuthenticated: !!getStoredToken(),
  error: null,

  initialize: async () => {
    const token = getStoredToken()
    if (token) {
      set({ token, isAuthenticated: true, isReady: true, authRequired: true })
      return
    }

    const required = await probeAuthRequired()
    set({
      isReady: true,
      authRequired: required,
      isAuthenticated: !required,
    })
  },

  login: async (username, password) => {
    set({ error: null })
    try {
      const token = await loginWithCredentials(username, password)
      set({ token, isAuthenticated: true, authRequired: true, error: null })
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Login failed'
      set({ error: message, isAuthenticated: false })
      throw err
    }
  },

  logout: () => {
    clearStoredToken()
    set({ token: null, isAuthenticated: false, error: null })
  },
}))