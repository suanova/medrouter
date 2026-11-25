import { api, type ApiEnvelope } from '@/lib/api'

export interface MatchedResources {
  image?: string
  cpu?: string
  memory?: string
  gpu: number
  storage?: string
  volume?: string
  dataset_size?: string
  idle_timeout_seconds?: number
  model_version?: string
  base_model?: string
  dataset?: string
  output?: string
  deadline_seconds?: number
}

export type WorkspacePhase =
  | 'planned'
  | 'creating'
  | 'running'
  | 'stopped'
  | 'succeeded'
  | 'merging'
  | 'ready'
  | 'failed'

export interface Workspace {
  id: number
  kind: 'notebook' | 'inference' | 'finetune'
  source_kind: 'model' | 'dataset'
  source_name: string
  name: string
  namespace: string
  phase: WorkspacePhase
  reason?: string
  endpoint?: string
  matched: MatchedResources
  /** Set by the server when STUDIO_DRY_RUN is on: nothing was created. */
  dry_run?: boolean
  created_at?: string
}

function unwrap<T>(envelope: ApiEnvelope<T>): T {
  if (!envelope.success || envelope.data === undefined) {
    throw new Error(envelope.message || 'Request failed')
  }
  return envelope.data
}

export async function fetchWorkspaces(): Promise<Workspace[]> {
  const response = await api.get<ApiEnvelope<{ workspaces: Workspace[] }>>('/studio/workspaces')
  return unwrap(response.data).workspaces ?? []
}

export const WORKSPACES_QUERY_KEY = ['studio', 'workspaces'] as const

export async function createWorkspace(
  sourceKind: 'model' | 'dataset',
  sourceName: string,
  datasetName?: string,
  /** ModelVersion to deploy; empty means the model's newest version. */
  version?: string
): Promise<Workspace> {
  const response = await api.post<ApiEnvelope<Workspace>>('/studio/workspaces', {
    source_kind: sourceKind,
    source_name: sourceName,
    ...(datasetName ? { dataset_name: datasetName } : {}),
    ...(version ? { version } : {}),
  })
  return unwrap(response.data)
}

export async function setWorkspaceRunning(id: number, running: boolean): Promise<Workspace> {
  const response = await api.post<ApiEnvelope<Workspace>>(
    `/studio/workspaces/${id}/${running ? 'start' : 'stop'}`
  )
  return unwrap(response.data)
}

/**
 * Signs the browser into a running notebook without asking for its token and
 * returns the URL to navigate to. The server relays only the session cookie.
 */
export async function openNotebook(id: number): Promise<string> {
  const response = await api.post<ApiEnvelope<{ url: string }>>(`/studio/workspaces/${id}/open`)
  return unwrap(response.data).url
}

export async function deleteWorkspace(id: number): Promise<void> {
  await api.delete(`/studio/workspaces/${id}`)
}
