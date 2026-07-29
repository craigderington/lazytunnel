import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { ImportReport, ImportValidationDetail } from '@/api/types'
import { tunnelKeys } from '@/lib/queries'
import { Button } from './ui/button'
import { Label } from './ui/label'
import { Switch } from './ui/switch'

const ACTION_STYLES: Record<string, string> = {
  create: 'text-[hsl(var(--live))]',
  update: 'text-foreground',
  skip: 'text-muted-foreground',
  delete: 'text-destructive',
}

function ReportItems({ report }: { report: ImportReport }) {
  return (
    <>
      <ul className="divide-y divide-border border-t border-border font-mono text-sm">
        {report.items.map((item) => (
          <li key={item.id || item.name} className="flex justify-between gap-4 py-2">
            <span className={ACTION_STYLES[item.action] ?? 'text-foreground'}>
              {item.action === 'delete' ? 'DELETE' : item.action}
            </span>
            <span className="flex-1 truncate">{item.name}</span>
            {item.error && <span className="text-destructive">{item.error}</span>}
          </li>
        ))}
      </ul>

      <p className="text-xs text-muted-foreground">
        {report.created} created, {report.updated} updated, {report.skipped} skipped,{' '}
        {report.deleted} deleted
        {report.failed > 0 && `, ${report.failed} failed`}
      </p>
    </>
  )
}

// A failed import may still carry a Report describing which items landed
// before the failure (see APIClientError.body / the 500 PARTIAL response).
// Without this the only thing the user gets on partial failure is a bare
// message with no indication of which tunnels were and weren't touched.
function reportFromError(err: unknown): ImportReport | null {
  const body = errorBody(err)
  if (!body || !('report' in body)) {
    return null
  }
  const report = (body as { report?: unknown }).report
  return report && typeof report === 'object' ? (report as ImportReport) : null
}

// A 400 archive-validation failure carries one entry per problem (see
// internal/backup.EntryError via internal/api/backup_handlers.go), so every
// bad field can be fixed in one pass instead of a resubmit-per-error cycle.
function detailsFromError(err: unknown): ImportValidationDetail[] | null {
  const body = errorBody(err)
  if (!body || !('details' in body)) {
    return null
  }
  const details = (body as { details?: unknown }).details
  return Array.isArray(details) ? (details as ImportValidationDetail[]) : null
}

function errorBody(err: unknown): Record<string, unknown> | null {
  if (!err || typeof err !== 'object' || !('body' in err)) {
    return null
  }
  const body = (err as { body?: unknown }).body
  return body && typeof body === 'object' ? (body as Record<string, unknown>) : null
}

export function BackupSection() {
  const queryClient = useQueryClient()
  const fileInput = useRef<HTMLInputElement>(null)
  const [replace, setReplace] = useState(false)
  const [preview, setPreview] = useState<ImportReport | null>(null)
  const [archive, setArchive] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [errorReport, setErrorReport] = useState<ImportReport | null>(null)
  const [errorDetails, setErrorDetails] = useState<ImportValidationDetail[] | null>(null)
  const [modeChanged, setModeChanged] = useState(false)

  const reset = () => {
    setPreview(null)
    setArchive(null)
    setError(null)
    setErrorReport(null)
    setErrorDetails(null)
    if (fileInput.current) {
      fileInput.current.value = ''
    }
  }

  const handleDownload = async () => {
    setError(null)
    setBusy(true)
    try {
      const blob = await api.exportConfig()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `lazytunnel-backup-${new Date().toISOString().slice(0, 10)}.json`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Export failed')
    } finally {
      setBusy(false)
    }
  }

  const handleFile = async (file: File) => {
    setBusy(true)
    setError(null)
    setErrorReport(null)
    setErrorDetails(null)
    setModeChanged(false)
    try {
      const parsed = JSON.parse(await file.text())
      setArchive(parsed)
      setPreview(await api.importConfig(parsed, { replace, dryRun: true }))
    } catch (err) {
      setArchive(null)
      setPreview(null)
      setErrorReport(reportFromError(err))
      setErrorDetails(detailsFromError(err))
      setError(err instanceof Error ? err.message : 'Could not read that file')
      // Browsers do not re-fire `change` when a file input's value is
      // unchanged, so without clearing it, re-selecting the identical file
      // after a transient failure would silently do nothing.
      if (fileInput.current) {
        fileInput.current.value = ''
      }
    } finally {
      setBusy(false)
    }
  }

  const handleApply = async () => {
    if (!archive) return
    setBusy(true)
    setError(null)
    setErrorReport(null)
    setErrorDetails(null)
    try {
      setPreview(await api.importConfig(archive, { replace }))
      setArchive(null)
      // A replace apply can delete tunnels outright; the list must reflect
      // that immediately rather than waiting out the 5s poll interval
      // (web/src/lib/queries.ts).
      queryClient.invalidateQueries({ queryKey: tunnelKeys.lists() })
    } catch (err) {
      setErrorReport(reportFromError(err))
      setErrorDetails(detailsFromError(err))
      setError(err instanceof Error ? err.message : 'Import failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="mb-10 space-y-4 border-t border-border pt-8">
      <p className="text-xs uppercase tracking-wider text-muted-foreground">Backup</p>

      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm">Download backup</p>
          <p className="text-xs text-muted-foreground">
            All tunnel definitions as JSON. Contains hostnames, usernames and key paths —
            no key material.
          </p>
        </div>
        <Button variant="outline" size="sm" disabled={busy} onClick={handleDownload}>
          Download
        </Button>
      </div>

      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm">Replace mode</p>
          <p className="text-xs text-muted-foreground">
            {replace
              ? 'Deletes tunnels missing from the backup'
              : 'Merge: updates and adds only, never deletes'}
          </p>
        </div>
        <Switch
          checked={replace}
          onCheckedChange={(value) => {
            setReplace(value)
            reset()
            setModeChanged(true)
          }}
        />
      </div>

      <div className="space-y-2">
        <Label className="text-xs text-muted-foreground">Restore from file</Label>
        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          disabled={busy}
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) void handleFile(file)
          }}
          className="block w-full text-sm text-muted-foreground file:mr-3 file:rounded-md file:border file:border-border file:bg-transparent file:px-3 file:py-1.5 file:text-sm file:text-foreground"
        />
        {modeChanged && (
          <p className="text-xs text-muted-foreground">
            Mode changed — re-select your file to preview it in {replace ? 'replace' : 'merge'}{' '}
            mode.
          </p>
        )}
      </div>

      {error && (
        <div className="space-y-3">
          <p className="text-sm text-destructive">{error}</p>
          {errorDetails && errorDetails.length > 0 && (
            <ul className="space-y-1 font-mono text-xs text-destructive">
              {errorDetails.map((d) => (
                <li key={`${d.index}-${d.field}`}>
                  {d.name ? `${d.name}: ` : ''}
                  {d.field}: {d.message}
                </li>
              ))}
            </ul>
          )}
          {errorReport && (
            <div className="space-y-3 border-t border-border pt-4">
              <p className="text-xs uppercase tracking-wider text-muted-foreground">
                Partial failure
              </p>
              <ReportItems report={errorReport} />
            </div>
          )}
        </div>
      )}

      {preview && (
        <div className="space-y-3 border-t border-border pt-4">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">
            {preview.dry_run ? 'Preview' : 'Applied'}
          </p>

          <ReportItems report={preview} />

          {preview.dry_run && (
            <div className="flex gap-2">
              <Button size="sm" disabled={busy} onClick={handleApply}>
                {preview.deleted > 0
                  ? `Apply and delete ${preview.deleted}`
                  : 'Apply'}
              </Button>
              <Button variant="ghost" size="sm" disabled={busy} onClick={reset}>
                Cancel
              </Button>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
