import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ImportReport } from '@/api/types'
import { BackupSection } from './BackupSection'

const exportConfig = vi.fn()
const importConfig = vi.fn()

vi.mock('@/api/client', () => ({
  api: {
    exportConfig: (...args: unknown[]) => exportConfig(...args),
    importConfig: (...args: unknown[]) => importConfig(...args),
  },
}))

const ARCHIVE = { version: 1, exported_at: '2026-07-29T08:40:12Z', source: 'test', tunnels: [] }

function report(overrides: Partial<ImportReport> = {}): ImportReport {
  return {
    mode: 'merge',
    dry_run: true,
    items: [
      { action: 'update', name: 'prod-db', id: 'id-1' },
      { action: 'create', name: 'staging-api', id: 'id-2' },
      { action: 'skip', name: 'socks-jump', id: 'id-3', reason: 'identical to stored tunnel' },
    ],
    created: 1,
    updated: 1,
    skipped: 1,
    deleted: 0,
    failed: 0,
    ...overrides,
  }
}

function selectArchiveFile() {
  const input = document.querySelector('input[type="file"]') as HTMLInputElement
  const file = new File([JSON.stringify(ARCHIVE)], 'backup.json', { type: 'application/json' })
  fireEvent.change(input, { target: { files: [file] } })
}

beforeEach(() => {
  exportConfig.mockReset()
  importConfig.mockReset()
  // jsdom implements neither of these.
  URL.createObjectURL = vi.fn(() => 'blob:stub')
  URL.revokeObjectURL = vi.fn()
})

// @testing-library/react's automatic afterEach cleanup only registers itself
// when it detects a global `afterEach` (vitest.config here doesn't enable
// `test.globals`), so without this, DOM from earlier tests accumulates and
// `document.querySelector('input[type="file"]')` grabs a stale element.
afterEach(cleanup)

describe('BackupSection', () => {
  it('downloads an archive when Download is clicked', async () => {
    exportConfig.mockResolvedValue(new Blob([JSON.stringify(ARCHIVE)]))
    render(<BackupSection />)

    fireEvent.click(screen.getByRole('button', { name: /download/i }))

    await waitFor(() => expect(exportConfig).toHaveBeenCalledTimes(1))
  })

  it('previews a selected file as a dry run without applying it', async () => {
    importConfig.mockResolvedValue(report())
    render(<BackupSection />)

    selectArchiveFile()

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(1))
    expect(importConfig).toHaveBeenCalledWith(ARCHIVE, { replace: false, dryRun: true })

    expect(await screen.findByText('prod-db')).toBeTruthy()
    expect(screen.getByText('staging-api')).toBeTruthy()
    expect(screen.getByText('socks-jump')).toBeTruthy()
  })

  it('applies the archive for real when Apply is clicked', async () => {
    importConfig.mockResolvedValue(report())
    render(<BackupSection />)

    selectArchiveFile()
    const apply = await screen.findByRole('button', { name: /^apply$/i })

    importConfig.mockResolvedValue(report({ dry_run: false }))
    fireEvent.click(apply)

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(2))
    expect(importConfig).toHaveBeenLastCalledWith(ARCHIVE, { replace: false })
  })

  it('passes replace mode through to the preview call', async () => {
    importConfig.mockResolvedValue(report({ mode: 'replace', deleted: 1 }))
    render(<BackupSection />)

    fireEvent.click(screen.getByRole('switch'))
    selectArchiveFile()

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(1))
    expect(importConfig).toHaveBeenCalledWith(ARCHIVE, { replace: true, dryRun: true })
  })

  it('warns how many tunnels a replace will delete', async () => {
    importConfig.mockResolvedValue(
      report({
        mode: 'replace',
        deleted: 2,
        items: [
          { action: 'delete', name: 'old-bastion', id: 'id-9', reason: 'not present in archive' },
          { action: 'delete', name: 'retired-db', id: 'id-8', reason: 'not present in archive' },
        ],
      })
    )
    render(<BackupSection />)

    fireEvent.click(screen.getByRole('switch'))
    selectArchiveFile()

    // The count must be on the button itself — this is the last thing shown
    // before tunnels are destroyed.
    expect(await screen.findByRole('button', { name: /delete 2/i })).toBeTruthy()
    expect(screen.getAllByText('DELETE')).toHaveLength(2)
  })

  it('surfaces an import failure instead of a preview', async () => {
    importConfig.mockRejectedValue(new Error('archive validation failed'))
    render(<BackupSection />)

    selectArchiveFile()

    expect(await screen.findByText(/archive validation failed/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /^apply$/i })).toBeNull()
  })

  it('surfaces a malformed file without calling the API', async () => {
    render(<BackupSection />)

    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['this is not json'], 'broken.json', { type: 'application/json' })
    fireEvent.change(input, { target: { files: [file] } })

    await waitFor(() => expect(screen.getByText(/JSON/i)).toBeTruthy())
    expect(importConfig).not.toHaveBeenCalled()
  })

  it('shows which tunnels landed when a partial-failure report comes back with the error', async () => {
    // Mirrors the 500 PARTIAL response shape: {code, message, report}, attached
    // to the thrown error's `body` field by APIClientError / parseError.
    const failure = Object.assign(new Error('import partially failed'), {
      body: {
        code: 'PARTIAL',
        message: 'import partially failed',
        report: report({
          dry_run: false,
          created: 0,
          updated: 1,
          skipped: 0,
          deleted: 0,
          failed: 1,
          items: [
            { action: 'update', name: 'prod-db', id: 'id-1' },
            {
              action: 'create',
              name: 'broken-tunnel',
              id: 'id-7',
              error: 'ssh: handshake failed',
            },
          ],
        }),
      },
    })
    importConfig.mockRejectedValue(failure)
    render(<BackupSection />)

    selectArchiveFile()

    expect(await screen.findByText(/import partially failed/i)).toBeTruthy()
    expect(screen.getByText('broken-tunnel')).toBeTruthy()
    expect(screen.getByText('ssh: handshake failed')).toBeTruthy()
  })
})
