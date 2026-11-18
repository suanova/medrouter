import { queryOptions } from '@tanstack/react-query'

import { CATALOG_RETRY_COUNT, catalogRetryDelay } from '@/lib/catalog-query'

import { fetchModelDetail, fetchModelList, ModelNotFoundError } from './api'

export const modelListQueryOptions = queryOptions({
  queryKey: ['models', 'list'],
  queryFn: fetchModelList,
  staleTime: 30_000,
  retry: CATALOG_RETRY_COUNT,
  retryDelay: catalogRetryDelay,
})

export function modelDetailQueryOptions(name: string) {
  return queryOptions({
    queryKey: ['models', 'detail', name],
    queryFn: () => fetchModelDetail(name),
    staleTime: 30_000,
    retry: (failureCount, error) =>
      !(error instanceof ModelNotFoundError) && failureCount < CATALOG_RETRY_COUNT,
    retryDelay: catalogRetryDelay,
  })
}
