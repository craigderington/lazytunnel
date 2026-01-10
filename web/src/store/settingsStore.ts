import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export interface Settings {
  apiBaseUrl: string;
  autoRefreshInterval: number; // seconds
  enableNotifications: boolean;
  enableSounds: boolean;
  maxRetries: number;
  defaultKeepAlive: number; // seconds
  defaultAutoReconnect: boolean;
}

interface SettingsStore {
  settings: Settings;
  updateSettings: (updates: Partial<Settings>) => void;
  resetSettings: () => void;
}

const defaultSettings: Settings = {
  apiBaseUrl: import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1',
  autoRefreshInterval: 5,
  enableNotifications: true,
  enableSounds: false,
  maxRetries: 3,
  defaultKeepAlive: 30,
  defaultAutoReconnect: true,
};

export const useSettingsStore = create<SettingsStore>()(
  persist(
    (set) => ({
      settings: defaultSettings,
      updateSettings: (updates) =>
        set((state) => ({
          settings: { ...state.settings, ...updates },
        })),
      resetSettings: () => set({ settings: defaultSettings }),
    }),
    {
      name: 'lazytunnel-settings',
    }
  )
);
