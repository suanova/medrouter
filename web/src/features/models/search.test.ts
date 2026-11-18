import { describe, expect, it } from 'vitest'

import { DEFAULT_MODEL_SORT, modelSearchSchema, resolveSort } from './search'

describe('modelSearchSchema', () => {
  it('accepts an empty search', () => {
    expect(modelSearchSchema.parse({})).toEqual({})
  })

  it('accepts arrays and keeps multi-select facets as lists', () => {
    expect(
      modelSearchSchema.parse({
        q: ' qwen ',
        task: ['分类', '检索'],
        evidence: ['L2'],
        sort: 'updated',
      })
    ).toEqual({
      q: 'qwen',
      task: ['分类', '检索'],
      evidence: ['L2'],
      sort: 'updated',
    })
  })

  it('upgrades a single string to a one-element list', () => {
    expect(modelSearchSchema.parse({ task: 'chat' })).toEqual({
      task: ['chat'],
    })
  })

  it('trims values and drops empty entries', () => {
    expect(
      modelSearchSchema.parse({ task: [' chat ', '  ', ''], license: [] })
    ).toEqual({ task: ['chat'], license: undefined })
  })

  it('drops invalid and unknown values instead of failing', () => {
    expect(modelSearchSchema.parse({ q: 12, sort: 'downloads' })).toEqual({
      q: undefined,
      sort: undefined,
    })
    expect(modelSearchSchema.parse({ q: 'x'.repeat(121) })).toEqual({ q: undefined })
    expect(modelSearchSchema.parse({ publisher: 'Meta', task: 12 })).toEqual({
      task: undefined,
    })
  })
})

describe('resolveSort', () => {
  it('falls back to featured for missing or unknown sorts', () => {
    expect(resolveSort(undefined)).toBe(DEFAULT_MODEL_SORT)
    expect(resolveSort('downloads')).toBe(DEFAULT_MODEL_SORT)
    expect(resolveSort('compatibility')).toBe('compatibility')
  })
})
