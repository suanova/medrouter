// Shared wire types and props for the dataset/model file browser. The backend
// returns the same shape for both libraries; paths are relative to the version
// root and the bucket/URI never reaches the browser.

import type { CatalogReason, StorageSummary } from '@/features/models/types'

export type { CatalogReason, StorageSummary }

export interface FilesStatus {
  available: boolean
  reason?: CatalogReason | string
  message?: string
  retryable: boolean
  generated_at: string
}

export interface FileEntry {
  name: string
  path: string
  type: 'file' | 'dir'
  size?: number
  last_modified?: string
}

export interface FileListing extends FilesStatus {
  ref: string
  version: string
  storage: string
  prefix: string
  parent?: string
  entries: FileEntry[]
  truncated: boolean
  next_cursor?: string
}

export interface FileDownload {
  available: boolean
  reason?: string
  message?: string
  retryable: boolean
  /** presign: fetch url directly; proxy: fetch the content endpoint. */
  mode?: 'presign' | 'proxy'
  size: number
  url?: string
  expires_at?: string
}

/**
 * One file described for preview. Text arrives inline (at most window_bytes);
 * image and pdf point at the same-origin content endpoint so the object store
 * stays private.
 */
export interface FilePreview {
  available: boolean
  reason?: string
  message?: string
  retryable: boolean
  kind: 'text' | 'image' | 'pdf' | 'unsupported'
  previewable: boolean
  size: number
  content_type?: string
  content?: string
  encoding?: string
  truncated: boolean
  window_bytes?: number
  content_url?: string
}

export interface FilePreviewParams {
  ref?: string
  path: string
}

export interface FileBrowserVersion {
  ref: string
  version: string
  is_latest: boolean
  storage: StorageSummary
}

export interface FilesPageParams {
  ref?: string
  prefix?: string
  cursor?: string
  limit?: number
}

export interface FileDownloadParams {
  ref?: string
  path: string
}