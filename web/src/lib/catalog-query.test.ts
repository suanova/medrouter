import { describe, expect, it } from 'vitest'

import { CATALOG_RETRY_COUNT, catalogRetryDelay } from './catalog-query'

describe('catalog retry policy', () => {
  it('retries more than once', () => {
    expect(CATALOG_RETRY_COUNT).toBeGreaterThan(1)
  })

  it('backs off exponentially and caps the delay', () => {
    expect(catalogRetryDelay(0)).toBe(1000)
    expect(catalogRetryDelay(1)).toBe(2000)
    expect(catalogRetryDelay(2)).toBe(4000)
    expect(catalogRetryDelay(4)).toBe(8000)
    expect(catalogRetryDelay(10)).toBe(8000)
  })
})
