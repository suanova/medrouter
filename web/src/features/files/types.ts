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
  url?: string
  expires_at?: string
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
