import { FileBrowserPanel } from '@/features/files/file-browser-panel'

import { fetchModelDownloadUrl, fetchModelFiles } from '../api'
import type { ModelDetail } from '../types'

interface ModelFilesPanelProps {
  detail: ModelDetail
}

export function ModelFilesPanel({ detail }: ModelFilesPanelProps) {
  return (
    <FileBrowserPanel
      name={detail.model}
      latestRef={detail.latest_ref}
      versions={detail.versions}
      namespace='models'
      tabPrefix='ml'
      fetchPage={(params) => fetchModelFiles(detail.model, params)}
      fetchDownload={(params) => fetchModelDownloadUrl(detail.model, params)}
    />
  )
}
