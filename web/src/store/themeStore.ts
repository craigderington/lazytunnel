import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type Theme = 'light' | 'dark'

interface ThemeStore {
  theme: Theme
  toggleTheme: () => void
  setTheme: (theme: Theme) => void
}

export const useThemeStore = create<ThemeStore>()(
  persist(
    (set) => ({
      theme: 'dark', // Default to dark mode
      toggleTheme: () =>
        set((state) => {
          const newTheme = state.theme === 'light' ? 'dark' : 'light'
          updateDocumentTheme(newTheme)
          return { theme: newTheme }
        }),
      setTheme: (theme) => {
        updateDocumentTheme(theme)
        set({ theme })
      },
    }),
    {
      name: 'lazytunnel-theme',
      onRehydrateStorage: () => (state) => {
        if (state) {
          updateDocumentTheme(state.theme)
        }
      },
    }
  )
)

function updateDocumentTheme(theme: Theme) {
  const root = document.documentElement
  if (theme === 'dark') {
    root.classList.add('dark')
  } else {
    root.classList.remove('dark')
  }
}

// Initialize theme on load
if (typeof window !== 'undefined') {
  const stored = localStorage.getItem('lazytunnel-theme')
  if (stored) {
    try {
      const { state } = JSON.parse(stored)
      updateDocumentTheme(state.theme)
    } catch (e) {
      // Default to dark mode if parsing fails
      updateDocumentTheme('dark')
    }
  } else {
    // Default to dark mode
    updateDocumentTheme('dark')
  }
}
