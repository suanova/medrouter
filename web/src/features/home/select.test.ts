import { describe, expect, it } from 'vitest'

import type { DatasetSummary } from '@/features/datasets/types'
import type { ModelSummary } from '@/features/models/types'

import {
  bestEvidence,
  datasetAccess,
  deliveryMode,
  evidenceRank,
  modelsForScene,
  pickFeaturedDatasets,
  pickFeaturedModels,
  statementState,
} from './select'

function model(overrides: Partial<ModelSummary> = {}): ModelSummary {
  return {
    model: 'model-a',
    display_name: 'Model A',
    publisher: '',
    publisher_initials: '',
    description: '',
    architecture: '',
    quantization: '',
    task: '',
    department: '',
    modality: '',
    evidence: '',
    license: '',
    distribution: '',
    departments: [],
    modalities: [],
    tasks: [],
    distributions: [],
    tags: [],
    usage_boundary: '',
    storage_strategy: '',
    latest_version: '',
    version_count: 1,
    compatible_profile_count: 0,
    used_by_count: 0,
    featured: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function dataset(overrides: Partial<DatasetSummary> = {}): DatasetSummary {
  return {
    dataset: 'dataset-a',
    display_name: 'Dataset A',
    publisher: '',
    publisher_initials: '',
    description: '',
    format: '',
    task: '',
    license: '',
    tags: [],
    access: '',
    deidentification: '',
    ethics: '',
    source_repo: '',
    usage_boundary: '',
    storage_strategy: '',
    latest_version: '',
    version_count: 1,
    featured: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('evidence rank', () => {
  it('ranks L0-L4 and ignores unknown levels', () => {
    expect(evidenceRank('L0')).toBe(0)
    expect(evidenceRank(' l3 ')).toBe(3)
    expect(evidenceRank('L9')).toBe(-1)
    expect(evidenceRank(undefined)).toBe(-1)
  })

  it('picks the highest evidence level in a set', () => {
    expect(bestEvidence([model({ evidence: 'L1' }), model({ evidence: 'L3' })])).toBe(
      'L3'
    )
    expect(bestEvidence([model({ evidence: 'L1' })])).toBe('L1')
    expect(bestEvidence([model()])).toBeUndefined()
  })
})

describe('pickFeaturedModels', () => {
  it('prefers featured, then evidence level, then recency', () => {
    const featured = model({ model: 'featured', featured: true, evidence: 'L0' })
    const l3 = model({ model: 'l3', evidence: 'L3', updated_at: '2026-01-01T00:00:00Z' })
    const l2New = model({ model: 'l2-new', evidence: 'L2', updated_at: '2026-06-01T00:00:00Z' })
    const l2Old = model({ model: 'l2-old', evidence: 'L2', updated_at: '2026-02-01T00:00:00Z' })

    expect(pickFeaturedModels([l2Old, l3, l2New, featured]).map((m) => m.model)).toEqual([
      'featured',
      'l3',
      'l2-new',
    ])
  })

  it('limits the result count', () => {
    const models = Array.from({ length: 5 }, (_, i) => model({ model: `m${i}` }))
    expect(pickFeaturedModels(models, 2)).toHaveLength(2)
  })
})

describe('modelsForScene', () => {
  it('uses OR inside a facet and AND across facets', () => {
    const lung = model({ model: 'lung', departments: ['影像'], tasks: ['检测'] })
    const pathology = model({ model: 'path', departments: ['病理'], tasks: ['分割'] })
    const other = model({ model: 'other', departments: ['药学'], tasks: ['检测'] })
    const models = [lung, pathology, other]

    expect(
      modelsForScene(models, { department: ['影像', '病理'], task: ['检测'] }).map(
        (m) => m.model
      )
    ).toEqual(['lung'])
    expect(
      modelsForScene(models, { department: ['影像', '病理'], task: ['检测', '分割'] }).map(
        (m) => m.model
      )
    ).toEqual(['lung', 'path'])
    expect(modelsForScene(models, { department: ['影像'], task: [] })).toHaveLength(1)
  })
})

describe('deliveryMode', () => {
  it('treats an API distribution as SaaS', () => {
    expect(deliveryMode(['下载', 'API'])).toBe('api')
    expect(deliveryMode(['api'])).toBe('api')
    expect(deliveryMode(['下载'])).toBe('download')
    expect(deliveryMode(undefined)).toBe('download')
  })
})

describe('pickFeaturedDatasets', () => {
  it('prefers featured then recency', () => {
    const old = dataset({ dataset: 'old', updated_at: '2026-01-01T00:00:00Z' })
    const featured = dataset({ dataset: 'featured', featured: true })
    const recent = dataset({ dataset: 'recent', updated_at: '2026-06-01T00:00:00Z' })
    expect(pickFeaturedDatasets([old, recent, featured]).map((d) => d.dataset)).toEqual([
      'featured',
      'recent',
      'old',
    ])
  })
})

describe('dataset badges', () => {
  it('derives the access level from free text', () => {
    expect(datasetAccess('公开 GCS / S3 镜像批量下载')).toBe('public')
    expect(datasetAccess('需要申请，签署数据协议')).toBe('request')
    expect(datasetAccess('仅限机构内使用')).toBe('restricted')
    expect(datasetAccess(undefined)).toBe('restricted')
  })

  it('compresses deidentification and ethics statements', () => {
    expect(statementState('不适用：蛋白序列预测结构')).toBe('notApplicable')
    expect(statementState('已按单细胞流程匿名化')).toBe('declared')
    expect(statementState('')).toBe('unknown')
  })
})
