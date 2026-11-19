import { describe, expect, it } from 'vitest'

import type { ModelSummary } from './types'
import {
  activeFilterCount,
  buildFacets,
  FACET_KEYS,
  filterModels,
  matchesKeywords,
  sortModels,
} from './filters'

export function model(overrides: Partial<ModelSummary> = {}): ModelSummary {
  return {
    model: 'qwen2.5-7b-instruct',
    display_name: 'Qwen2.5-7B-Instruct',
    publisher: 'Alibaba Cloud',
    publisher_initials: 'AC',
    description: 'Chat model',
    architecture: 'qwen2',
    quantization: 'bf16',
    task: 'chat',
    department: '',
    modality: '',
    evidence: '',
    license: 'Apache-2.0',
    distribution: '',
    departments: [],
    modalities: [],
    tasks: ['chat'],
    distributions: [],
    tags: ['chat', 'qwen'],
    usage_boundary: '',
    storage_strategy: 'S3',
    latest_version: 'v1.2',
    version_count: 2,
    compatible_profile_count: 1,
    used_by_count: 0,
    featured: false,
    created_at: '2026-09-01T08:00:00Z',
    updated_at: '2026-10-01T08:00:00Z',
    ...overrides,
  }
}

const models = [
  model(),
  model({
    model: 'llama-3-8b',
    display_name: 'Llama 3 8B',
    publisher: 'Meta',
    publisher_initials: 'ME',
    description: 'General assistant',
    architecture: 'llama',
    quantization: 'w8a8',
    task: 'text',
    tasks: ['text'],
    license: 'Llama-3',
    tags: ['llama'],
    compatible_profile_count: 0,
    featured: true,
    updated_at: '2026-11-01T08:00:00Z',
  }),
  model({
    model: 'medgemma-4b',
    display_name: 'MedGemma 4B',
    publisher: 'Google Health',
    publisher_initials: 'GH',
    architecture: 'gemma',
    quantization: 'bf16',
    task: 'chat',
    license: 'Apache-2.0',
    tags: ['medical'],
    compatible_profile_count: 2,
    updated_at: '2026-10-15T08:00:00Z',
  }),
]

describe('filterModels', () => {
  it('matches keywords across name, publisher and tags', () => {
    expect(matchesKeywords(models[0], 'alibaba')).toBe(true)
    expect(matchesKeywords(models[2], 'medical')).toBe(true)
    expect(matchesKeywords(models[1], 'qwen')).toBe(false)
    expect(filterModels(models, { q: 'google' })).toEqual([models[2]])
  })

  it('combines facets with AND semantics and multi-selects with OR', () => {
    expect(filterModels(models, { task: ['chat'] })).toHaveLength(2)
    expect(filterModels(models, { task: ['chat', 'text'] })).toHaveLength(3)
    expect(
      filterModels(models, { task: ['chat'], license: ['Apache-2.0'] })
    ).toHaveLength(2)
    expect(
      filterModels(models, { task: ['text'], license: ['Apache-2.0'] })
    ).toHaveLength(0)
    expect(
      filterModels(models, { task: ['chat'], department: ['cardio'] })
    ).toHaveLength(0)
  })
})

describe('sortModels', () => {
  it('sorts featured first by default', () => {
    expect(sortModels(models)[0].model).toBe('llama-3-8b')
  })

  it('sorts by updated time', () => {
    expect(sortModels(models, 'updated').map((m) => m.model)).toEqual([
      'llama-3-8b',
      'medgemma-4b',
      'qwen2.5-7b-instruct',
    ])
  })

  it('sorts by name and compatibility', () => {
    expect(sortModels(models, 'name').map((m) => m.model)).toEqual([
      'llama-3-8b',
      'medgemma-4b',
      'qwen2.5-7b-instruct',
    ])
    expect(sortModels(models, 'compatibility')[0].model).toBe('medgemma-4b')
  })

  it('does not mutate the input', () => {
    const original = models.map((m) => m.model)
    sortModels(models, 'name')
    expect(models.map((m) => m.model)).toEqual(original)
  })
})

describe('facets', () => {
  it('counts distinct values and drops empty ones', () => {
    const facets = buildFacets(models)
    expect(facets.task).toEqual([
      { value: 'chat', count: 2 },
      { value: 'text', count: 1 },
    ])
    expect(facets.department).toEqual([])
    expect(facets.license[0]).toEqual({ value: 'Apache-2.0', count: 2 })
  })

  it('places evidence directly under department', () => {
    expect(FACET_KEYS.slice(0, 2)).toEqual(['department', 'evidence'])
  })

  it('orders evidence from L4 down to L0, not by count', () => {
    const set = [
      model({ evidence: 'L1' }),
      model({ evidence: 'L1' }),
      model({ evidence: 'L3' }),
      model({ evidence: 'L0' }),
      model({ evidence: 'L2' }),
    ]
    expect(buildFacets(set).evidence.map((facet) => facet.value)).toEqual([
      'L3',
      'L2',
      'L1',
      'L0',
    ])
  })

  it('counts every selected value plus search', () => {
    expect(activeFilterCount({})).toBe(0)
    expect(activeFilterCount({ q: '  ', task: [] })).toBe(0)
    expect(activeFilterCount({ q: 'x', task: ['chat'] })).toBe(2)
    expect(
      activeFilterCount({ task: ['chat', 'text'], license: ['Apache-2.0'] })
    ).toBe(3)
  })

  it('builds list facets from medicine annotations and filters by membership', () => {
    const medical = model({
      model: 'biomistral-7b',
      display_name: 'BioMistral',
      department: '心血管,呼吸',
      departments: ['cardio', 'resp'],
      modality: 'EMR文本',
      modalities: ['EMR'],
      task: '分类',
      tasks: ['chat', 'vqa'],
      evidence: 'L2',
      license: 'Apache-2.0',
      distribution: '下载，API',
      distributions: ['download', 'api'],
    })
    const generic = model()
    const set = [medical, generic]
    expect(buildFacets(set).department).toContainEqual({ value: 'cardio', count: 1 })
    expect(buildFacets(set).modality).toContainEqual({ value: 'EMR', count: 1 })
    expect(buildFacets(set).evidence).toContainEqual({ value: 'L2', count: 1 })
    expect(filterModels(set, { department: ['cardio'] })).toEqual([medical])
    expect(filterModels(set, { task: ['chat'] })).toHaveLength(2)
    expect(filterModels(set, { task: ['vqa'] })).toEqual([medical])
    expect(filterModels(set, { task: ['chat', 'vqa'] })).toHaveLength(2)
    expect(
      filterModels(set, { department: ['cardio'], task: ['vqa'], evidence: ['L2'] })
    ).toEqual([medical])
  })
})
