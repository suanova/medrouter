import { useInfiniteQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import {
  ChevronRight,
  Download,
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
import { basePath } from '@/lib/base-path'
import { CATALOG_RETRY_COUNT, catalogRetryDelay } from '@/lib/catalog-query'
import { useAuthStore } from '@/stores/auth-store'

import type {
  FileBrowserVersion,
  FileDownload,
  FileDownloadParams,
  FileEntry,
  FileListing,
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
}

// Only S3-backed versions with a declared URI can be browsed online.
function browsableVersions(versions: FileBrowserVersion[]): FileBrowserVersion[] {
  return versions.filter(
    (version) => version.storage.strategy.toUpperCase() === 'S3' && version.storage.uri_configured
  )
}

function formatBytes(value: number | undefined, locale: string): string {
  if (value == null) return '—'
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let size = value / 1024
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${new Intl.NumberFormat(locale === 'zh' ? 'zh-CN' : 'en', {
    maximumFractionDigits: size >= 10 ? 0 : 1,
  }).format(size)} ${units[index]}`
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
      if (!download.available || !download.url) {
        toast.error(
          t(`${labels}.reasons.${download.reason ?? 'default'}`, {
            defaultValue: download.message || t(`${labels}.reasons.default`),
          })
        )
        return
      }
      window.location.assign(download.url)
    } catch (error) {
      toast.error(apiErrorMessage(error) ?? t(`${labels}.downloadFailed`))
    } finally {
      setDownloading(null)
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
    </section>
  )
}
