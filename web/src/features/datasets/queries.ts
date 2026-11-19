import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query'

import { CATALOG_RETRY_COUNT, catalogRetryDelay } from '@/lib/catalog-query'

import {
  DatasetNotFoundError,
  fetchDatasetDetail,
  fetchDatasetFiles,
  fetchDatasetList,
} from './api'

export const datasetListQueryOptions = queryOptions({
  queryKey: ['datasets', 'list'],
  queryFn: fetchDatasetList,
  staleTime: 30_000,
  retry: CATALOG_RETRY_COUNT,
  retryDelay: catalogRetryDelay,
})

export function datasetDetailQueryOptions(name: string) {
  return queryOptions({
    queryKey: ['datasets', 'detail', name],
    queryFn: () => fetchDatasetDetail(name),
    staleTime: 30_000,
    retry: (failureCount, error) =>
      !(error instanceof DatasetNotFoundError) && failureCount < CATALOG_RETRY_COUNT,
    retryDelay: catalogRetryDelay,
  })
}

// datasetFilesQueryOptions paginates one directory level. The cursor is the
// value the API returned as next_cursor; the first page has no cursor.
export function datasetFilesQueryOptions(name: string, ref: string, prefix: string) {
  return infiniteQueryOptions({
    queryKey: ['datasets', 'files', name, ref, prefix],
    queryFn: ({ pageParam }) =>
      fetchDatasetFiles(name, { ref, prefix, cursor: pageParam, limit: 200 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.truncated ? lastPage.next_cursor : undefined,
    staleTime: 20_000,
    retry: CATALOG_RETRY_COUNT,
    retryDelay: catalogRetryDelay,
  })
}
