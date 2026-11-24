import { useQuery } from '@tanstack/react-query'

import { api, type ApiEnvelope } from '@/lib/api'

export interface FineTuneDataset {
  name: string
  format: string
  size?: string
  record_count?: number
  task: string
  annotated: boolean
}

export interface ExcludedDataset {
  name: string
  task?: string
  reason: string
}

export interface FineTuneTargets {
  model: string
  available: boolean
  reason?: string
  method?: string
  tasks: string[]
  annotated: boolean
  datasets: FineTuneDataset[]
  excluded: ExcludedDataset[]
}

function unwrap<T>(envelope: ApiEnvelope<T>): T {
  if (!envelope.success || envelope.data === undefined) {
    throw new Error(envelope.message || 'Request failed')
  }
  return envelope.data
}

export async function fetchFineTuneTargets(model: string): Promise<FineTuneTargets> {
  const response = await api.get<ApiEnvelope<FineTuneTargets>>('/studio/finetune/targets', {
    params: { model },
  })
  return unwrap(response.data)
}

export function fineTuneTargetsQueryOptions(model: string) {
  return {
    queryKey: ['studio', 'finetune', model] as const,
    queryFn: () => fetchFineTuneTargets(model),
    staleTime: 30_000,
    enabled: Boolean(model),
  }
}

export function useFineTuneTargets(model: string) {
  return useQuery(fineTuneTargetsQueryOptions(model))
}
