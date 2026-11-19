import type { DatasetSummary } from '@/features/datasets/types'
import { filterModels, type ModelFilterState } from '@/features/models/filters'
import type { ModelSummary } from '@/features/models/types'

// The homepage sorts and filters catalog data client-side: the catalog is
// small (tens of models), so the API can stay as-is and the scene counts stay
// consistent with the model page's filter semantics.

const EVIDENCE_RANK: Record<string, number> = {
  L0: 0,
  L1: 1,
  L2: 2,
  L3: 3,
  L4: 4,
}

export function evidenceRank(level: string | undefined): number {
  const key = level?.trim().toUpperCase()
  return key ? (EVIDENCE_RANK[key] ?? -1) : -1
}

export function bestEvidence(models: ModelSummary[]): string | undefined {
  let best: string | undefined
  let rank = -1
  for (const model of models) {
    const value = model.evidence?.trim()
    if (!value) continue
    const current = evidenceRank(value)
    if (current > rank) {
      best = value
      rank = current
    }
  }
  return best
}

export function pickFeaturedModels(models: ModelSummary[], count = 3): ModelSummary[] {
  return [...models]
    .sort(
      (a, b) =>
        Number(b.featured) - Number(a.featured) ||
        evidenceRank(b.evidence) - evidenceRank(a.evidence) ||
        updatedTime(b.updated_at) - updatedTime(a.updated_at)
    )
    .slice(0, count)
}

export function modelsForScene(
  models: ModelSummary[],
  filters: ModelFilterState
): ModelSummary[] {
  return filterModels(models, filters)
}

export type DeliveryMode = 'api' | 'download'

// deliveryMode derives the passport's delivery badge from the model's
// distributions: an API distribution means the platform can serve it as SaaS.
export function deliveryMode(distributions: string[] | undefined): DeliveryMode {
  return (distributions ?? []).some((value) => /api/i.test(value))
    ? 'api'
    : 'download'
}

export function pickFeaturedDatasets(
  datasets: DatasetSummary[],
  count = 3
): DatasetSummary[] {
  return [...datasets]
    .sort(
      (a, b) =>
        Number(b.featured) - Number(a.featured) ||
        updatedTime(b.updated_at) - updatedTime(a.updated_at)
    )
    .slice(0, count)
}

export type AccessLevel = 'public' | 'request' | 'restricted'

export function datasetAccess(access: string | undefined): AccessLevel {
  const value = (access ?? '').trim().toLowerCase()
  if (!value) return 'restricted'
  if (/(公开|public)/.test(value)) return 'public'
  if (/(申请|受邀|request|invite|gated)/.test(value)) return 'request'
  return 'restricted'
}

export type StatementState = 'notApplicable' | 'declared' | 'unknown'

// statementState compresses the free-text deidentification / ethics fields
// into a badge. The raw text stays available as the badge's tooltip.
export function statementState(value: string | undefined): StatementState {
  const text = (value ?? '').trim()
  if (!text) return 'unknown'
  if (/^(不适用|无需|none|n\/a)/i.test(text)) return 'notApplicable'
  return 'declared'
}

function updatedTime(value: string): number {
  const time = Date.parse(value)
  return Number.isNaN(time) ? 0 : time
}
