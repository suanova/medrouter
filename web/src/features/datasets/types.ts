// Wire types for GET /api/datasets and GET /api/datasets/:name. They mirror
// the sanitized DTOs in internal/service/datasetcatalog_dto.go: storage paths,
// S3 URIs and credential names are never exposed.

import type { CatalogReason, StorageSummary } from '@/features/models/types'

export type { CatalogReason, StorageSummary }

export interface DatasetCatalogStatus {
  available: boolean
  reason?: CatalogReason | string
  message?: string
  retryable: boolean
  stale: boolean
  warning?: string
  generated_at: string
}

export interface DatasetSummary {
  dataset: string
  display_name: string
  publisher: string
  publisher_initials: string
  description: string
  format: string
  task: string
  license: string
  tags: string[]
  access: string
  deidentification: string
  ethics: string
  source_repo: string
  usage_boundary: string
  size?: string
  record_count?: number
  storage_strategy: string
  latest_version: string
  version_count: number
  featured: boolean
  created_at: string
  updated_at: string
}

export interface DatasetList extends DatasetCatalogStatus {
  total: number
  version_total: number
  datasets: DatasetSummary[]
}

export interface DatasetVersionDetail {
  ref: string
  version: string
  is_latest: boolean
  format: string
  size?: string
  record_count?: number
  description: string
  storage: StorageSummary
  created_at: string
}

export interface DatasetOverviewBenchmark {
  name: string
  metric: string
  value: string
  split?: string
  source?: string
}

export interface DatasetOverview {
  dataset: string
  description?: string
  intended_use?: string
  limitations?: string[]
  contraindications?: string[]
  out_of_scope?: string[]
  benchmarks?: DatasetOverviewBenchmark[]
  regulatory_status?: string
  regulatory_note?: string
  disclaimer?: string
  links?: Record<string, string>
  extra?: Record<string, unknown>
}

export interface DatasetDetail extends DatasetCatalogStatus, DatasetSummary {
  latest_ref: string
  versions: DatasetVersionDetail[]
  overview?: DatasetOverview
}

// Wire types for GET /api/datasets/:name/files. Paths are relative to the
// version root; the bucket and S3 URI are never exposed.

// Wire types for GET /api/datasets/:name/files. The listing shape is shared
// with the model file browser; these aliases keep the dataset API imports
// stable.
export type {
  FilesStatus as DatasetFilesStatus,
  FileEntry as DatasetFileEntry,
  FileListing as DatasetFiles,
  FileDownload as DatasetDownload,
} from '@/features/files/types'
