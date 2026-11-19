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
import { useAuthStore } from '@/stores/auth-store'

import { fetchDatasetDownloadUrl } from '../api'
import { datasetFilesQueryOptions } from '../queries'
import type { DatasetDetail, DatasetFileEntry, DatasetVersionDetail } from '../types'

interface DatasetFilesPanelProps {
  detail: DatasetDetail
}

// Only S3-backed versions with a declared URI can be browsed online.
function browsableVersions(detail: DatasetDetail): DatasetVersionDetail[] {
  return detail.versions.filter(
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

export function DatasetFilesPanel({ detail }: DatasetFilesPanelProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.user)
  const initialized = useAuthStore((state) => state.initialized)
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'

  const versions = useMemo(() => browsableVersions(detail), [detail])
  const [ref, setRef] = useState(() => {
    const preferred = versions.find((version) => version.ref === detail.latest_ref)
    return preferred?.ref ?? versions[0]?.ref ?? ''
  })
  const [prefix, setPrefix] = useState('')
  const [downloading, setDownloading] = useState<string | null>(null)

  const filesQuery = useInfiniteQuery({
    ...datasetFilesQueryOptions(detail.dataset, ref, prefix),
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

  const startDownload = async (entry: DatasetFileEntry) => {
    if (!user) {
      void navigate({ to: '/sign-in', search: { redirect: currentSitePath() } })
      return
    }
    setDownloading(entry.path)
    try {
      const download = await fetchDatasetDownloadUrl(detail.dataset, { ref, path: entry.path })
      if (!download.available || !download.url) {
        toast.error(
          t(`datasets.files.reasons.${download.reason ?? 'default'}`, {
            defaultValue: download.message || t('datasets.files.reasons.default'),
          })
        )
        return
      }
      window.location.assign(download.url)
    } catch (error) {
      toast.error(apiErrorMessage(error) ?? t('datasets.files.downloadFailed'))
    } finally {
      setDownloading(null)
    }
  }

  if (versions.length === 0) {
    return (
      <section className='ml-panel' role='tabpanel' id='ds-panel-files' aria-labelledby='ds-tab-files'>
        <div className='ml-empty ml-empty-compact'>
          <strong>{t('datasets.files.unsupportedTitle')}</strong>
          <span>{t('datasets.files.unsupportedText')}</span>
        </div>
      </section>
    )
  }

  const unavailable = status !== undefined && !status.available
  const loadingFirstPage = filesQuery.isPending && !status

  return (
    <section className='ml-panel' role='tabpanel' id='ds-panel-files' aria-labelledby='ds-tab-files'>
      <div className='ml-panel-head ds-files-head'>
        <h2>{t('datasets.files.title')}</h2>
        <label className='ds-files-version'>
          <span>{t('datasets.files.version')}</span>
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
                {version.is_latest ? ` · ${t('datasets.fields.latestBadge')}` : ''}
              </option>
            ))}
          </select>
        </label>
      </div>

      <nav className='ds-files-crumbs' aria-label={t('datasets.files.aria')}>
        <button type='button' onClick={() => openDirectory('')}>
          {t('datasets.files.root')}
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
          <strong>{t('datasets.files.unavailableTitle')}</strong>
          <span>{t('datasets.files.reasons.default')}</span>
          <button
            type='button'
            className='ml-btn ml-btn-secondary'
            onClick={() => void filesQuery.refetch()}
          >
            <RotateCw aria-hidden='true' />
            {t('datasets.files.retry')}
          </button>
        </div>
      ) : null}

      {status && !status.available && !filesQuery.isError ? (
        <div className='ml-empty ml-empty-compact' role='status'>
          <strong>{t('datasets.files.unavailableTitle')}</strong>
          <span>
            {t(`datasets.files.reasons.${status.reason ?? 'default'}`, {
              defaultValue: status.message || t('datasets.files.reasons.default'),
            })}
          </span>
          {status.retryable ? (
            <button
              type='button'
              className='ml-btn ml-btn-secondary'
              onClick={() => void filesQuery.refetch()}
            >
              <RotateCw aria-hidden='true' />
              {t('datasets.files.retry')}
            </button>
          ) : null}
        </div>
      ) : null}

      {!loadingFirstPage && !unavailable && !filesQuery.isError ? (
        entries.length === 0 ? (
          <div className='ml-empty ml-empty-compact'>
            <strong>{t('datasets.files.emptyTitle')}</strong>
            <span>{t('datasets.files.emptyText')}</span>
          </div>
        ) : (
          <div className='ds-files-list' role='table' aria-label={t('datasets.files.aria')}>
            <div className='ds-files-row ds-files-row-head' role='row'>
              <span role='columnheader'>{t('datasets.files.name')}</span>
              <span role='columnheader'>{t('datasets.files.size')}</span>
              <span role='columnheader'>{t('datasets.files.modified')}</span>
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
                      {user ? t('datasets.files.download') : t('datasets.files.downloadAfterSignIn')}
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
            {t('datasets.files.loadMore')}
          </button>
        </div>
      ) : null}

      {status && activeVersion ? (
        <p className='ml-side-note ds-files-note'>
          {t('datasets.files.storageNote', {
            version: status.version,
            storage: status.storage,
          })}
        </p>
      ) : null}
    </section>
  )
}
