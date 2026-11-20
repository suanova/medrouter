import { useInfiniteQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import {
  ChevronRight,
  Download,
  Eye,
  FileText,
  Folder,
  Loader2,
  Lock,
  RotateCw,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { formatDate } from '@/features/models/format'
import { apiErrorMessage } from '@/lib/api'
import { apiBase, basePath } from '@/lib/base-path'
import { CATALOG_RETRY_COUNT, catalogRetryDelay } from '@/lib/catalog-query'
import { useAuthStore } from '@/stores/auth-store'

import { FilePreviewDialog } from './file-preview-dialog'
import { formatBytes } from './format'
import type {
  FileBrowserVersion,
  FileDownload,
  FileDownloadParams,
  FileEntry,
  FileListing,
  FilePreview,
  FilePreviewParams,
  FilesPageParams,
} from './types'

interface FileBrowserPanelProps {
  name: string
  latestRef: string
  versions: FileBrowserVersion[]
  /** datasets | models — selects the i18n namespace. */
  namespace: 'datasets' | 'models'
  /** ds | ml — must match the detail page tab ids. */
  tabPrefix: string
  fetchPage: (params: FilesPageParams) => Promise<FileListing>
  fetchDownload: (params: FileDownloadParams) => Promise<FileDownload>
  fetchPreview: (params: FilePreviewParams) => Promise<FilePreview>
}

// Only S3-backed versions with a declared URI can be browsed online.
function browsableVersions(versions: FileBrowserVersion[]): FileBrowserVersion[] {
  return versions.filter(
    (version) => version.storage.strategy.toUpperCase() === 'S3' && version.storage.uri_configured
  )
}

// Text-like files never need a round trip to decide whether "Open" applies;
// the backend still makes the authoritative call.
const TEXT_PREVIEW_EXTENSIONS = new Set([
  'json', 'jsonl', 'ndjson', 'txt', 'md', 'markdown', 'csv', 'tsv', 'yaml', 'yml',
  'toml', 'ini', 'cfg', 'conf', 'log', 'py', 'go', 'sh', 'js', 'ts', 'tsx', 'jsx',
  'java', 'c', 'cpp', 'h', 'hpp', 'rs', 'sql', 'proto', 'xml', 'html', 'htm',
  'css', 'svg', 'mod', 'sum', 'patch', 'diff',
])
const IMAGE_PREVIEW_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp'])

// canPreview is a hint only: it decides whether the button looks active.
function canPreview(entry: FileEntry): boolean {
  if (entry.type !== 'file') return false
  const name = entry.name.toLowerCase()
  const dot = name.lastIndexOf('.')
  if (dot < 0) {
    return ['readme', 'license', 'notice', 'changelog', 'makefile', 'dockerfile'].includes(name)
  }
  const extension = name.slice(dot + 1)
  return TEXT_PREVIEW_EXTENSIONS.has(extension) || IMAGE_PREVIEW_EXTENSIONS.has(extension) || extension === 'pdf'
}

// currentSitePath returns the router path (without BASE_PATH) so the sign-in
// redirect does not double the prefix.
function currentSitePath(): string {
  const pathname = window.location.pathname
  if (basePath && pathname.startsWith(basePath)) {
    return pathname.slice(basePath.length) || '/'
  }
  return pathname || '/'
}

export function FileBrowserPanel({
  name,
  latestRef,
  versions: allVersions,
  namespace,
  tabPrefix,
  fetchPage,
  fetchDownload,
  fetchPreview,
}: FileBrowserPanelProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.user)
  const initialized = useAuthStore((state) => state.initialized)
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const labels = `${namespace}.files`

  const versions = useMemo(() => browsableVersions(allVersions), [allVersions])
  const [ref, setRef] = useState(() => {
    const preferred = versions.find((version) => version.ref === latestRef)
    return preferred?.ref ?? versions[0]?.ref ?? ''
  })
  const [prefix, setPrefix] = useState('')
  const [downloading, setDownloading] = useState<string | null>(null)
  const [previewing, setPreviewing] = useState<string | null>(null)
  const [preview, setPreview] = useState<{ entry: FileEntry; data: FilePreview } | null>(null)

  const filesQuery = useInfiniteQuery({
    queryKey: ['files', namespace, name, ref, prefix],
    queryFn: ({ pageParam }) => fetchPage({ ref, prefix, cursor: pageParam, limit: 200 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.truncated ? lastPage.next_cursor : undefined,
    staleTime: 20_000,
    retry: CATALOG_RETRY_COUNT,
    retryDelay: catalogRetryDelay,
    enabled: Boolean(ref),
  })

  const pages = filesQuery.data?.pages ?? []
  const status = pages[0]
  const entries = pages.flatMap((page) => page.entries)
  const activeVersion = versions.find((version) => version.ref === ref)

  const segments = prefix.split('/').filter(Boolean)
  const crumbs = segments.map((segment, index) => ({
    label: segment,
    prefix: `${segments.slice(0, index + 1).join('/')}/`,
  }))

  const openDirectory = (nextPrefix: string) => {
    setPrefix(nextPrefix)
  }

  const startDownload = async (entry: FileEntry) => {
    if (!user) {
      void navigate({ to: '/sign-in', search: { redirect: currentSitePath() } })
      return
    }
    setDownloading(entry.path)
    try {
      const download = await fetchDownload({ ref, path: entry.path })
      if (!download.available || (!download.url && download.mode !== 'proxy')) {
        toast.error(
          t(`${labels}.reasons.${download.reason ?? 'default'}`, {
            defaultValue: download.message || t(`${labels}.reasons.default`),
          })
        )
        return
      }
      if (download.mode === 'proxy') {
        // Bytes stream through MedRouter; the content endpoint enforces the
        // per-file and per-user limits on the way out.
        const query = new URLSearchParams({ path: entry.path, download: '1' })
        if (ref) query.set('ref', ref)
        window.location.assign(
          `${apiBase}/${namespace}/${encodeURIComponent(name)}/files/content?${query.toString()}`
        )
        return
      }
      window.location.assign(download.url as string)
    } catch (error) {
      toast.error(apiErrorMessage(error) ?? t(`${labels}.downloadFailed`))
    } finally {
      setDownloading(null)
    }
  }

  const startPreview = async (entry: FileEntry) => {
    if (!user) {
      void navigate({ to: '/sign-in', search: { redirect: currentSitePath() } })
      return
    }
    setPreviewing(entry.path)
    try {
      const data = await fetchPreview({ ref, path: entry.path })
      if (!data.available && data.reason) {
        toast.error(
          t(`${labels}.reasons.${data.reason}`, {
            defaultValue: data.message || t(`${labels}.reasons.default`),
          })
        )
        return
      }
      setPreview({ entry, data })
    } catch (error) {
      toast.error(apiErrorMessage(error) ?? t(`${labels}.previewFailed`))
    } finally {
      setPreviewing(null)
    }
  }

  if (versions.length === 0) {
    return (
      <section
        className='ml-panel'
        role='tabpanel'
        id={`${tabPrefix}-panel-files`}
        aria-labelledby={`${tabPrefix}-tab-files`}
      >
        <div className='ml-empty ml-empty-compact'>
          <strong>{t(`${labels}.unsupportedTitle`)}</strong>
          <span>{t(`${labels}.unsupportedText`)}</span>
        </div>
      </section>
    )
  }

  const unavailable = status !== undefined && !status.available
  const loadingFirstPage = filesQuery.isPending && !status

  return (
    <section
      className='ml-panel'
      role='tabpanel'
      id={`${tabPrefix}-panel-files`}
      aria-labelledby={`${tabPrefix}-tab-files`}
    >
      <div className='ml-panel-head ds-files-head'>
        <h2>{t(`${labels}.title`)}</h2>
        <label className='ds-files-version'>
          <span>{t(`${labels}.version`)}</span>
          <select
            value={ref}
            onChange={(event) => {
              setRef(event.target.value)
              setPrefix('')
            }}
          >
            {versions.map((version) => (
              <option key={version.ref} value={version.ref}>
                {version.version}
                {version.is_latest ? ` · ${t(`${namespace}.fields.latestBadge`)}` : ''}
              </option>
            ))}
          </select>
        </label>
      </div>

      <nav className='ds-files-crumbs' aria-label={t(`${labels}.aria`)}>
        <button type='button' onClick={() => openDirectory('')}>
          {t(`${labels}.root`)}
        </button>
        {crumbs.map((crumb) => (
          <span className='ds-files-crumb' key={crumb.prefix}>
            <ChevronRight aria-hidden='true' />
            <button type='button' onClick={() => openDirectory(crumb.prefix)}>
              {crumb.label}
            </button>
          </span>
        ))}
      </nav>

      {loadingFirstPage ? (
        <div className='ds-files-list' aria-busy='true'>
          {[0, 1, 2].map((index) => (
            <div className='ds-files-row' key={index}>
              <span className='ml-skeleton' style={{ width: 220, height: 12 }} />
            </div>
          ))}
        </div>
      ) : null}

      {filesQuery.isError ? (
        <div className='ml-empty ml-empty-compact' role='status'>
          <strong>{t(`${labels}.unavailableTitle`)}</strong>
          <span>{t(`${labels}.reasons.default`)}</span>
          <button
            type='button'
            className='ml-btn ml-btn-secondary'
            onClick={() => void filesQuery.refetch()}
          >
            <RotateCw aria-hidden='true' />
            {t(`${labels}.retry`)}
          </button>
        </div>
      ) : null}

      {status && !status.available && !filesQuery.isError ? (
        <div className='ml-empty ml-empty-compact' role='status'>
          <strong>{t(`${labels}.unavailableTitle`)}</strong>
          <span>
            {t(`${labels}.reasons.${status.reason ?? 'default'}`, {
              defaultValue: status.message || t(`${labels}.reasons.default`),
            })}
          </span>
          {status.retryable ? (
            <button
              type='button'
              className='ml-btn ml-btn-secondary'
              onClick={() => void filesQuery.refetch()}
            >
              <RotateCw aria-hidden='true' />
              {t(`${labels}.retry`)}
            </button>
          ) : null}
        </div>
      ) : null}

      {!loadingFirstPage && !unavailable && !filesQuery.isError ? (
        entries.length === 0 ? (
          <div className='ml-empty ml-empty-compact'>
            <strong>{t(`${labels}.emptyTitle`)}</strong>
            <span>{t(`${labels}.emptyText`)}</span>
          </div>
        ) : (
          <div className='ds-files-list' role='table' aria-label={t(`${labels}.aria`)}>
            <div className='ds-files-row ds-files-row-head' role='row'>
              <span role='columnheader'>{t(`${labels}.name`)}</span>
              <span role='columnheader'>{t(`${labels}.size`)}</span>
              <span role='columnheader'>{t(`${labels}.modified`)}</span>
              <span role='columnheader' aria-hidden='true' />
            </div>

            {entries.map((entry) => (
              <div className='ds-files-row' role='row' key={entry.path}>
                <span className='ds-files-name' role='cell'>
                  {entry.type === 'dir' ? (
                    <button type='button' onClick={() => openDirectory(entry.path)}>
                      <Folder aria-hidden='true' />
                      <span>{entry.name}</span>
                    </button>
                  ) : (
                    <span className='ds-files-file'>
                      <FileText aria-hidden='true' />
                      <span>{entry.name}</span>
                    </span>
                  )}
                </span>
                <span className='ds-files-meta' role='cell'>
                  {entry.type === 'file' ? formatBytes(entry.size, locale) : '—'}
                </span>
                <span className='ds-files-meta' role='cell'>
                  {entry.type === 'file' ? formatDate(entry.last_modified, locale) : '—'}
                </span>
                <span className='ds-files-action' role='cell'>
                  {entry.type === 'file' ? (
                    <span className='ds-files-actions'>
                      <button
                        type='button'
                        className='ml-btn ml-btn-secondary ds-files-open'
                        disabled={!initialized || previewing === entry.path || !canPreview(entry)}
                        title={
                          canPreview(entry)
                            ? t(`${labels}.open`)
                            : t(`${labels}.previewUnsupportedHint`)
                        }
                        onClick={() => void startPreview(entry)}
                      >
                        {previewing === entry.path ? (
                          <Loader2 className='ds-files-spin' aria-hidden='true' />
                        ) : (
                          <Eye aria-hidden='true' />
                        )}
                        {t(`${labels}.open`)}
                      </button>
                      <button
                        type='button'
                        className='ml-btn ml-btn-secondary ds-files-download'
                        disabled={!initialized || downloading === entry.path}
                        onClick={() => void startDownload(entry)}
                      >
                        {downloading === entry.path ? (
                          <Loader2 className='ds-files-spin' aria-hidden='true' />
                        ) : user ? (
                          <Download aria-hidden='true' />
                        ) : (
                          <Lock aria-hidden='true' />
                        )}
                        {user
                          ? t(`${labels}.download`)
                          : t(`${labels}.downloadAfterSignIn`)}
                      </button>
                    </span>
                  ) : null}
                </span>
              </div>
            ))}
          </div>
        )
      ) : null}

      {filesQuery.hasNextPage && !unavailable ? (
        <div className='ds-files-more'>
          <button
            type='button'
            className='ml-btn ml-btn-secondary'
            disabled={filesQuery.isFetchingNextPage}
            onClick={() => void filesQuery.fetchNextPage()}
          >
            {filesQuery.isFetchingNextPage ? (
              <Loader2 className='ds-files-spin' aria-hidden='true' />
            ) : null}
            {t(`${labels}.loadMore`)}
          </button>
        </div>
      ) : null}

      {status && activeVersion ? (
        <p className='ml-side-note ds-files-note'>
          {t(`${labels}.storageNote`, {
            version: status.version,
            storage: status.storage,
          })}
        </p>
      ) : null}

      {preview ? (
        <FilePreviewDialog
          entry={preview.entry}
          preview={preview.data}
          labels={labels}
          onClose={() => setPreview(null)}
        />
      ) : null}
    </section>
  )
}
