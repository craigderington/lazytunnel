import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ImportReport } from './types'
import { api, APIClientError } from './client'

// Real auth is out of scope here — this test is only about parseError()
// preserving the response body. Node 22's experimental global `localStorage`
// otherwise gets exercised (and throws) before jsdom's ever would.
vi.mock('@/lib/auth', () => ({
  getAuthToken: vi.fn().mockResolvedValue(null),
  clearStoredToken: vi.fn(),
}))

// Unlike BackupSection.test.tsx (which mocks '@/api/client' wholesale),
// this exercises the real request()/parseError() path with the module
// unmocked. That's the point: the mandated fix is that parseError attaches
// the *whole* response body to APIClientError.body instead of keeping only
// message/code, so a 500 IMPORT_PARTIAL_FAILURE's report is no longer
// silently discarded. A test that mocks the client can't see that fix at
// all — only a test against the real client can.
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('parseError', () => {
  it('attaches the full parsed error body, including a partial-failure report, to the thrown APIClientError', async () => {
    const report: ImportReport = {
      mode: 'replace',
      dry_run: false,
      items: [
        { action: 'create', name: 'broken-tunnel', id: 'id-7', error: 'ssh: handshake failed' },
      ],
      created: 0,
      updated: 0,
      skipped: 0,
      deleted: 0,
      failed: 1,
    }
    const responseBody = {
      code: 'IMPORT_PARTIAL_FAILURE',
      message: 'import partially failed: 1 of 1 item(s)',
      report,
    }

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        json: async () => responseBody,
      })
    )

    let caught: unknown
    try {
      await api.importConfig({ version: 1, tunnels: [] }, { replace: true })
    } catch (err) {
      caught = err
    }

    expect(caught).toBeInstanceOf(APIClientError)
    const err = caught as APIClientError
    expect(err.message).toBe(responseBody.message)
    expect(err.code).toBe(responseBody.code)
    // The report must survive intact, not just be present as some truthy value.
    expect((err.body as typeof responseBody).report).toEqual(report)
  })
})
