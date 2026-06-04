import { api } from '@/api/client'
import { apiUrl } from '@/lib/config'

const TOKEN_KEY = 'lazytunnel_token'

export function getStoredToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function clearStoredToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

export async function loginWithCredentials(
  username: string,
  password: string
): Promise<string> {
  const data = await api.login({ username, password })
  localStorage.setItem(TOKEN_KEY, data.token)
  return data.token
}

/** Returns stored token only — does not auto-login. */
export async function getAuthToken(): Promise<string | null> {
  return getStoredToken()
}

/** Probe whether the API requires authentication. */
export async function probeAuthRequired(): Promise<boolean> {
  const response = await fetch(apiUrl('/tunnels'))
  return response.status === 401
}