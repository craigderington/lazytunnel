import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ImportReport } from '@/api/types'
import { tunnelKeys } from '@/lib/queries'
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

// BackupSection calls useQueryClient() (to invalidate the tunnel list after a
// successful apply), so every render needs a real provider — a bare
// `render(<BackupSection />)` would throw "No QueryClient set". Returning the
// client lets tests spy on invalidateQueries.
function renderSection() {
  const queryClient = new QueryClient()
  render(
    <QueryClientProvider client={queryClient}>
      <BackupSection />
    </QueryClientProvider>
  )
  return queryClient
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
    renderSection()

    fireEvent.click(screen.getByRole('button', { name: /download/i }))

    await waitFor(() => expect(exportConfig).toHaveBeenCalledTimes(1))
  })

  it('previews a selected file as a dry run without applying it', async () => {
    importConfig.mockResolvedValue(report())
    renderSection()

    selectArchiveFile()

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(1))
    expect(importConfig).toHaveBeenCalledWith(ARCHIVE, { replace: false, dryRun: true })

    expect(await screen.findByText('prod-db')).toBeTruthy()
    expect(screen.getByText('staging-api')).toBeTruthy()
    expect(screen.getByText('socks-jump')).toBeTruthy()
  })

  it('applies the archive for real when Apply is clicked', async () => {
    importConfig.mockResolvedValue(report())
    renderSection()

    selectArchiveFile()
    const apply = await screen.findByRole('button', { name: /^apply$/i })

    importConfig.mockResolvedValue(report({ dry_run: false }))
    fireEvent.click(apply)

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(2))
    expect(importConfig).toHaveBeenLastCalledWith(ARCHIVE, { replace: false })
  })

  it('invalidates the tunnel list cache after a successful apply', async () => {
    importConfig.mockResolvedValue(report())
    const queryClient = renderSection()
    const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries')

    selectArchiveFile()
    const apply = await screen.findByRole('button', { name: /^apply$/i })

    importConfig.mockResolvedValue(report({ dry_run: false }))
    fireEvent.click(apply)

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(2))
    expect(invalidateQueries).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: tunnelKeys.lists() })
    )
  })

  it('passes replace mode through to the preview call', async () => {
    importConfig.mockResolvedValue(report({ mode: 'replace', deleted: 1 }))
    renderSection()

    fireEvent.click(screen.getByRole('switch'))
    selectArchiveFile()

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(1))
    expect(importConfig).toHaveBeenCalledWith(ARCHIVE, { replace: true, dryRun: true })
  })

  it('hints that the file must be re-selected after switching modes', async () => {
    renderSection()

    fireEvent.click(screen.getByRole('switch'))

    expect(await screen.findByText(/re-select your file/i)).toBeTruthy()
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
    renderSection()

    fireEvent.click(screen.getByRole('switch'))
    selectArchiveFile()

    // The count must be on the button itself — this is the last thing shown
    // before tunnels are destroyed.
    expect(await screen.findByRole('button', { name: /delete 2/i })).toBeTruthy()
    expect(screen.getAllByText('DELETE')).toHaveLength(2)
  })

  it('surfaces an import failure instead of a preview', async () => {
    importConfig.mockRejectedValue(new Error('archive validation failed'))
    renderSection()

    selectArchiveFile()

    expect(await screen.findByText(/archive validation failed/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /^apply$/i })).toBeNull()
  })

  it('surfaces a malformed file without calling the API', async () => {
    renderSection()

    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['this is not json'], 'broken.json', { type: 'application/json' })
    fireEvent.change(input, { target: { files: [file] } })

    // getByText(/JSON/i) alone would also match the always-present static
    // description ("All tunnel definitions as JSON…"), so this scopes to the
    // error paragraph and to wording only a JSON.parse failure produces.
    await waitFor(() => {
      const message = document.querySelector('.text-destructive')
      expect(message?.textContent).toMatch(/not valid JSON/i)
    })
    expect(importConfig).not.toHaveBeenCalled()
  })

  it('lets the same file be re-selected after a failed preview', async () => {
    // jsdom does not reproduce the real-browser restriction this guards
    // against (a native file pick sets a fake path into `.value`, which
    // scripts can only ever reset back to '' — never re-set to a path — and
    // real browsers refuse to re-fire `change` while `.value` is unchanged).
    // fireEvent.change() dispatches unconditionally, and jsdom never
    // populates `.value` from a script-assigned `files` list in the first
    // place, so neither a call-count assertion nor reading `.value` after
    // the fact would fail if the reset line were deleted. Spying on the
    // setter is what actually pins the component to calling
    // `fileInput.current.value = ''` on failure.
    importConfig.mockRejectedValueOnce(new Error('network error'))
    renderSection()

    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    const valueSetter = vi.spyOn(input, 'value', 'set')

    selectArchiveFile()
    await screen.findByText(/network error/i)

    expect(valueSetter).toHaveBeenCalledWith('')

    importConfig.mockResolvedValueOnce(report())
    selectArchiveFile()

    await waitFor(() => expect(importConfig).toHaveBeenCalledTimes(2))
  })

  it('shows which tunnels landed when a partial-failure report comes back with the error', async () => {
    // Mirrors the 500 IMPORT_PARTIAL_FAILURE response shape:
    // {code, message, report}, attached to the thrown error's `body` field by
    // APIClientError / parseError (internal/api/backup_handlers.go:165-169).
    const failure = Object.assign(new Error('import partially failed'), {
      body: {
        code: 'IMPORT_PARTIAL_FAILURE',
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
    renderSection()

    selectArchiveFile()

    expect(await screen.findByText(/import partially failed/i)).toBeTruthy()
    expect(screen.getByText('broken-tunnel')).toBeTruthy()
    expect(screen.getByText('ssh: handshake failed')).toBeTruthy()
  })

  it('shows every per-entry validation problem from a 400 archive-validation failure', async () => {
    // Mirrors internal/api/backup_handlers.go:120-124's {code, message, details}
    // shape, where details is []backup.EntryError.
    const failure = Object.assign(new Error('archive validation failed: 1 problem(s)'), {
      body: {
        code: 'VALIDATION_ERROR',
        message: 'archive validation failed: 1 problem(s)',
        details: [
          {
            index: 0,
            name: 'bad-tunnel',
            field: 'hops[0].host',
            message: 'must not be empty',
          },
        ],
      },
    })
    importConfig.mockRejectedValue(failure)
    renderSection()

    selectArchiveFile()

    expect(await screen.findByText(/archive validation failed/i)).toBeTruthy()
    expect(screen.getByText(/must not be empty/i)).toBeTruthy()
  })
})
