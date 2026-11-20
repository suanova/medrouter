import { Copy, ExternalLink, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatBytes } from './format'
import type { FileEntry, FilePreview } from './types'

interface FilePreviewDialogProps {
  entry: FileEntry
  preview: FilePreview
  /** datasets | models — selects the i18n namespace. */
  labels: string
  onClose: () => void
}

/**
 * Text previews arrive inline in the JSON response, so they are rendered as a
 * text node and never parsed as HTML. Images and PDFs are loaded from the
 * same-origin content endpoint, which keeps the object store private.
 */
export function FilePreviewDialog({
  entry,
  preview,
  labels,
  onClose,
}: FilePreviewDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const [copied, setCopied] = useState(false)
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    closeRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  const copy = async () => {
    if (!preview.content) return
    try {
      await navigator.clipboard.writeText(preview.content)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div
      className='ds-preview-backdrop'
      role='presentation'
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        className='ds-preview-dialog'
        role='dialog'
        aria-modal='true'
        aria-label={t(`${labels}.previewTitle`, { name: entry.name })}
      >
        <header className='ds-preview-head'>
          <div className='ds-preview-title'>
            <strong>{entry.name}</strong>
            <span>{formatBytes(preview.size, locale)}</span>
          </div>
          <div className='ds-preview-actions'>
            {preview.kind === 'text' && preview.content ? (
              <button type='button' className='ml-btn ml-btn-secondary' onClick={() => void copy()}>
                <Copy aria-hidden='true' />
                {copied ? t(`${labels}.copied`) : t(`${labels}.copy`)}
              </button>
            ) : null}
            <button
              type='button'
              ref={closeRef}
              className='ml-btn ml-btn-secondary'
              onClick={onClose}
            >
              <X aria-hidden='true' />
              {t(`${labels}.close`)}
            </button>
          </div>
        </header>

        {!preview.previewable ? (
          <div className='ml-empty ml-empty-compact' role='status'>
            <strong>{t(`${labels}.previewUnavailableTitle`)}</strong>
            <span>
              {t(`${labels}.reasons.${preview.reason ?? 'default'}`, {
                defaultValue: preview.message || t(`${labels}.reasons.default`),
              })}
            </span>
          </div>
        ) : null}

        {preview.previewable && preview.truncated ? (
          <p className='ds-preview-note' role='status'>
            {t(`${labels}.previewTruncated`, {
              shown: formatBytes(preview.window_bytes, locale),
              total: formatBytes(preview.size, locale),
            })}
          </p>
        ) : null}

        {preview.previewable && preview.kind === 'text' ? (
          <pre className='ds-preview-text'>{preview.content ?? ''}</pre>
        ) : null}

        {preview.previewable && preview.kind === 'image' && preview.content_url ? (
          <div className='ds-preview-media'>
            <img src={preview.content_url} alt={entry.name} />
          </div>
        ) : null}

        {preview.previewable && preview.kind === 'pdf' && preview.content_url ? (
          <div className='ds-preview-media'>
            <iframe
              className='ds-preview-pdf'
              src={preview.content_url}
              title={entry.name}
            />
            <a
              className='ml-btn ml-btn-secondary'
              href={preview.content_url}
              target='_blank'
              rel='noreferrer'
            >
              <ExternalLink aria-hidden='true' />
              {t(`${labels}.openInNewTab`)}
            </a>
          </div>
        ) : null}
      </div>
    </div>
  )
}
