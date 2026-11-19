import { FileBrowserPanel } from '@/features/files/file-browser-panel'

import { fetchDatasetDownloadUrl, fetchDatasetFiles } from '../api'
import type { DatasetDetail } from '../types'

interface DatasetFilesPanelProps {
  detail: DatasetDetail
}

export function DatasetFilesPanel({ detail }: DatasetFilesPanelProps) {
  return (
    <FileBrowserPanel
      name={detail.dataset}
      latestRef={detail.latest_ref}
      versions={detail.versions}
      namespace='datasets'
      tabPrefix='ds'
      fetchPage={(params) => fetchDatasetFiles(detail.dataset, params)}
      fetchDownload={(params) => fetchDatasetDownloadUrl(detail.dataset, params)}
    />
  )
}
