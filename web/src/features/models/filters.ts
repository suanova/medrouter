import type { ModelSummary } from './types'
import { DEFAULT_MODEL_SORT, type ModelSort } from './search'

// ModelFilterState holds the multi-select facets. Each facet is a list: a model
// matches when ANY of its values for that facet is selected (OR inside one
// facet), and every selected facet must match (AND across facets).
export interface ModelFilterState {
  q?: string
  department?: string[]
  modality?: string[]
  task?: string[]
  evidence?: string[]
  license?: string[]
}

// FACET_KEYS is the curated, user-facing filter set. It is deliberately small:
// these are the dimensions a medical-model shopper chooses by. Publisher is
// covered by the free-text search; architecture, quantization, distribution and
// usage boundary stay on the detail page instead of cluttering the sidebar.
export const FACET_KEYS = [
  'department',
  'evidence',
  'modality',
  'task',
  'license',
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
  license: 'license',
}

// LIST_FACET_FIELDS read an array-valued model field (each element becomes one
// filter option). task maps to the tasks/list field so multi-task models match
// any single task value.
const LIST_FACET_FIELDS: Record<string, keyof ModelSummary> = {
  department: 'departments',
  modality: 'modalities',
  task: 'tasks',
}

// selectedValues normalizes a facet selection: trims, drops empty entries and
// tolerates a missing list.
export function selectedValues(values: string[] | undefined): string[] {
  return (values ?? []).map((value) => value.trim()).filter(Boolean)
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
      const wanted = selectedValues(filters[key])
      if (wanted.length === 0) continue
      const values = facetValues(model, key)
      if (!values.some((value) => wanted.includes(value))) return false
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

// evidenceRank orders the evidence facet from the strongest level (L4) down to
// L0, independent of how many models claim each level.
function evidenceRank(value: string): number {
  const match = /^L(\d+)$/i.exec(value.trim())
  return match ? Number(match[1]) : -1
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
    const values = [...counts.entries()].map(([value, count]) => ({ value, count }))
    facets[key] =
      key === 'evidence'
        ? values.sort(
            (a, b) =>
              evidenceRank(b.value) - evidenceRank(a.value) ||
              a.value.localeCompare(b.value)
          )
        : values.sort((a, b) => b.count - a.count || a.value.localeCompare(b.value))
  }
  return facets
}

// activeFilterCount counts every selected value plus the free-text search, so
// the toolbar badge reflects how many narrowing conditions are in effect.
export function activeFilterCount(filters: ModelFilterState): number {
  let count = filters.q?.trim() ? 1 : 0
  for (const key of FACET_KEYS) {
    count += selectedValues(filters[key]).length
  }
  return count
}

export function hasFacet(facets: Facets): boolean {
  return FACET_KEYS.some((key) => facets[key].length > 0)
}
