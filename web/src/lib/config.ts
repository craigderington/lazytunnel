/** API base URL — use relative path in dev (Vite proxy) and production. */
export const API_BASE_URL =
  import.meta.env.VITE_API_URL?.replace(/\/$/, '') || '/api/v1'

export function apiUrl(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${API_BASE_URL}${normalized}`
}

export function wsUrl(path = '/ws'): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  const base = API_BASE_URL.replace(/^http/, 'ws')
  return `${base}${normalized}`
}