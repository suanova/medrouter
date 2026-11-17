import type { ModelSummary } from './types'
import { DEFAULT_MODEL_SORT, type ModelSort } from './search'

export interface ModelFilterState {
  q?: string
  department?: string
  modality?: string
  task?: string
  evidence?: string
  publisher?: string
  license?: string
  distribution?: string
  arch?: string
  quant?: string
  usage?: string
}

export const FACET_KEYS = [
  'department',
  'modality',
  'task',
  'evidence',
  'publisher',
  'license',
  'distribution',
  'arch',
  'quant',
  'usage',
] as const

export type FacetKey = (typeof FACET_KEYS)[number]

export interface FacetValue {
  value: string
  count: number
}

export type Facets = Record<FacetKey, FacetValue[]>

// SINGLE_FACET_FIELDS carry a single string value.
const SINGLE_FACET_FIELDS: Record<string, keyof ModelSummary> = {
  evidence: 'evidence',
  publisher: 'publisher',
  arch: 'architecture',
  quant: 'quantization',
  license: 'license',
  usage: 'usage_boundary',
}

// LIST_FACET_FIELDS read an array-valued model field (each element becomes one
// filter option). task maps to the tasks/list field so multi-task models match
// any single task value.
const LIST_FACET_FIELDS: Record<string, keyof ModelSummary> = {
  department: 'departments',
  modality: 'modalities',
  task: 'tasks',
  distribution: 'distributions',
}

// facetValues returns the facet values a model contributes for a key: exactly
// one for single-valued fields, one per element for list fields.
function facetValues(model: ModelSummary, key: FacetKey): string[] {
  const listKey = LIST_FACET_FIELDS[key]
  if (listKey) {
    const list = model[listKey] as string[] | undefined
    return (list ?? []).map((v) => v.trim()).filter(Boolean)
  }
  const raw = model[SINGLE_FACET_FIELDS[key]]
  const value = typeof raw === 'string' ? raw.trim() : ''
  return value ? [value] : []
}

export function matchesKeywords(model: ModelSummary, query: string | undefined): boolean {
  const q = query?.trim().toLowerCase()
  if (!q) return true
  const haystack = [
    model.display_name,
    model.model,
    model.publisher,
    model.description,
    model.task,
    model.architecture,
    model.quantization,
    model.license,
    model.usage_boundary,
    ...model.tags,
    ...(model.departments ?? []),
    ...(model.modalities ?? []),
    ...(model.tasks ?? []),
    ...(model.distributions ?? []),
  ]
    .join(' ')
    .toLowerCase()
  return haystack.includes(q)
}

export function filterModels(
  models: ModelSummary[],
  filters: ModelFilterState
): ModelSummary[] {
  return models.filter((model) => {
    if (!matchesKeywords(model, filters.q)) return false
    for (const key of FACET_KEYS) {
      const wanted = filters[key]?.trim()
      if (wanted && !facetValues(model, key).includes(wanted)) return false
    }
    return true
  })
}

function updatedTime(model: ModelSummary): number {
  const value = Date.parse(model.updated_at)
  return Number.isNaN(value) ? 0 : value
}

export function sortModels(
  models: ModelSummary[],
  sort: ModelSort = DEFAULT_MODEL_SORT
): ModelSummary[] {
  const copy = [...models]
  switch (sort) {
    case 'name':
      return copy.sort((a, b) =>
        a.display_name.localeCompare(b.display_name, undefined, {
          sensitivity: 'base',
          numeric: true,
        })
      )
    case 'updated':
      return copy.sort((a, b) => updatedTime(b) - updatedTime(a))
    case 'compatibility':
      return copy.sort(
        (a, b) =>
          b.compatible_profile_count - a.compatible_profile_count ||
          updatedTime(b) - updatedTime(a)
      )
    case 'featured':
    default:
      return copy.sort(
        (a, b) =>
          Number(b.featured) - Number(a.featured) || updatedTime(b) - updatedTime(a)
      )
  }
}

export function buildFacets(models: ModelSummary[]): Facets {
  const facets: Facets = Object.fromEntries(
    FACET_KEYS.map((key) => [key, [] as FacetValue[]])
  ) as Facets
  for (const key of FACET_KEYS) {
    const counts = new Map<string, number>()
    for (const model of models) {
      for (const value of facetValues(model, key)) {
        if (!value) continue
        counts.set(value, (counts.get(value) ?? 0) + 1)
      }
    }
    facets[key] = [...counts.entries()]
      .map(([value, count]) => ({ value, count }))
      .sort(
        (a, b) => b.count - a.count || a.value.localeCompare(b.value)
      )
  }
  return facets
}

export function activeFilterCount(filters: ModelFilterState): number {
  let count = filters.q?.trim() ? 1 : 0
  for (const key of FACET_KEYS) {
    if (filters[key]?.trim()) count += 1
  }
  return count
}

export function hasFacet(facets: Facets): boolean {
  return FACET_KEYS.some((key) => facets[key].length > 0)
}
