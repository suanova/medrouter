import { z } from 'zod'

export const MODEL_SORTS = ['featured', 'updated', 'name', 'compatibility'] as const

export type ModelSort = (typeof MODEL_SORTS)[number]

export const DEFAULT_MODEL_SORT: ModelSort = 'featured'

const filterValue = z.string().trim().max(120).optional().catch(undefined)

// filterList accepts both the JSON array the router serializes for a
// multi-select facet and the bare string older single-value links used, so
// ?task=分类&task=检索 and ?task=分类 both keep working. Empty values are dropped.
const filterList = z
  .union([z.string(), z.array(z.string())])
  .optional()
  .transform((value) => {
    if (value === undefined) return undefined
    const list = (Array.isArray(value) ? value : [value])
      .map((item) => item.trim())
      .filter(Boolean)
    return list.length > 0 ? list : undefined
  })
  .catch(undefined)

// Search parameters are shareable. Unknown or malformed values fall back to
// "not set" instead of making the page fail to render.
export const modelSearchSchema = z.object({
  q: filterValue,
  department: filterList,
  modality: filterList,
  task: filterList,
  evidence: filterList,
  license: filterList,
  sort: z.enum(MODEL_SORTS).optional().catch(undefined),
})

export type ModelSearch = z.infer<typeof modelSearchSchema>

export function resolveSort(value: string | undefined): ModelSort {
  return MODEL_SORTS.includes(value as ModelSort)
    ? (value as ModelSort)
    : DEFAULT_MODEL_SORT
}
