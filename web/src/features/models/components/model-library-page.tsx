import { getRouteApi } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { RotateCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SiteHeader } from '@/components/site-header'

import {
  activeFilterCount,
  buildFacets,
  filterModels,
  hasFacet,
  selectedValues,
  sortModels,
  type FacetKey,
  type ModelFilterState,
} from '../filters'
import { formatRelativeTime } from '../format'
import { modelListQueryOptions } from '../queries'
import { resolveSort, type ModelSearch } from '../search'
import { CatalogLoading } from './loading-states'
import { CatalogToolbar } from './catalog-toolbar'
import { EmptyState } from './empty-state'
import { FilterSidebar } from './filter-sidebar'
import { ModelCard } from './model-card'
import { NoticeBar } from './notice-bar'
import { PageHead } from './page-head'
import { Spotlight } from './spotlight'
import { UnavailableState } from './unavailable-state'

const routeApi = getRouteApi('/models/')

function newestModel<T extends { updated_at: string }>(models: T[]): T {
  return [...models].sort(
    (a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)
  )[0]
}

export function ModelLibraryPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const search = routeApi.useSearch()
  const navigate = routeApi.useNavigate()
  const { data, isPending, isError, isFetching, refetch } = useQuery(
    modelListQueryOptions
  )
  const [filtersOpen, setFiltersOpen] = useState(false)

  const filters: ModelFilterState = useMemo(
    () => ({
      q: search.q,
      department: search.department,
      modality: search.modality,
      task: search.task,
      evidence: search.evidence,
      license: search.license,
    }),
    [
      search.q,
      search.department,
      search.modality,
      search.task,
      search.evidence,
      search.license,
    ]
  )
  const sort = resolveSort(search.sort)
  const activeCount = activeFilterCount(filters)

  const facets = useMemo(() => buildFacets(data?.models ?? []), [data])
  const visible = useMemo(
    () => sortModels(filterModels(data?.models ?? [], filters), sort),
    [data, filters, sort]
  )

  const updateSearch = (patch: Partial<ModelSearch>) => {
    void navigate({
      search: (previous) => ({ ...previous, ...patch }),
      replace: true,
    })
  }

  const setFacet = (key: FacetKey, values: string[]) => {
    const patch: Partial<ModelSearch> = {}
    patch[key] = values.length > 0 ? values : undefined
    updateSearch(patch)
  }

  // Toggling is the multi-select primitive: clicking a selected option removes
  // it, clicking an unselected one adds it, and a facet leaves the URL only
  // when its last value is gone.
  const toggleFacet = (key: FacetKey, value: string) => {
    const current = selectedValues(filters[key])
    setFacet(
      key,
      current.includes(value)
        ? current.filter((item) => item !== value)
        : [...current, value]
    )
  }

  const clearFacetValue = (key: FacetKey, value: string) => {
    setFacet(key, selectedValues(filters[key]).filter((item) => item !== value))
  }

  const clearFacet = (key: FacetKey) => setFacet(key, [])

  const resetFilters = () => {
    updateSearch({
      q: undefined,
      department: undefined,
      modality: undefined,
      task: undefined,
      evidence: undefined,
      license: undefined,
    })
  }

  const spotlight = useMemo(() => {
    if (!data || data.models.length === 0) return null
    return data.models.find((model) => model.featured) ?? newestModel(data.models)
  }, [data])

  return (
    <div className='ml-scope'>
      <SiteHeader />
      <main className='ml-main'>
        <div className='ml-container'>
          <PageHead
            kicker={t('models.kicker')}
            title={t('models.title')}
            lead={t('models.lead')}
            notes={
              data?.available
                ? [
                    t('models.headNotes.models', { count: data.total }),
                    t('models.headNotes.versions', { count: data.version_total }),
                    ...(data.profiles_available
                      ? [
                          t('models.headNotes.profiles', {
                            count: data.profile_total,
                          }),
                        ]
                      : []),
                  ]
                : []
            }
            actions={
              <button
                type='button'
                className='ml-btn ml-btn-secondary'
                onClick={() => void refetch()}
                disabled={isFetching}
              >
                <RotateCw aria-hidden='true' />
                {isFetching ? t('models.refreshing') : t('models.refresh')}
              </button>
            }
          />

          {isPending ? <CatalogLoading /> : null}

          {isError ? (
            <UnavailableState
              reason='API_ERROR'
              retryable
              onRetry={() => void refetch()}
            />
          ) : null}

          {data && !data.available ? (
            <UnavailableState
              reason={data.reason}
              retryable={data.retryable}
              onRetry={() => void refetch()}
            />
          ) : null}

          {data?.available ? (
            <>
              {spotlight ? <Spotlight model={spotlight} /> : null}

              {data.stale ? (
                <NoticeBar label={t('models.states.warning')} warning>
                  {t('models.states.stale', {
                    time: formatRelativeTime(data.generated_at, locale),
                  })}
                </NoticeBar>
              ) : null}

              {!data.profiles_available ? (
                <NoticeBar label={t('models.states.warning')} warning>
                  {t('models.states.profileUnavailable')}
                </NoticeBar>
              ) : null}

              <NoticeBar label={t('models.notice.label')}>
                {t('models.notice.text')}
              </NoticeBar>

              <div className='ml-catalog-shell'>
                {hasFacet(facets) || activeCount > 0 ? (
                  <FilterSidebar
                    facets={facets}
                    filters={filters}
                    open={filtersOpen}
                    onToggle={toggleFacet}
                    onClear={clearFacet}
                    onReset={resetFilters}
                    onClose={() => setFiltersOpen(false)}
                  />
                ) : null}

                <div className='ml-catalog-main'>
                  <CatalogToolbar
                    query={search.q ?? ''}
                    onQueryChange={(value) => updateSearch({ q: value || undefined })}
                    resultCount={visible.length}
                    total={data.total}
                    filtered={activeCount > 0}
                    activeCount={activeCount}
                    filters={filters}
                    onClearQuery={() => updateSearch({ q: undefined })}
                    onClearFacet={clearFacetValue}
                    onReset={resetFilters}
                    sort={sort}
                    onSortChange={(value) =>
                      updateSearch({ sort: value === 'featured' ? undefined : value })
                    }
                    filtersOpen={filtersOpen}
                    onToggleFilters={() => setFiltersOpen((open) => !open)}
                  />

                  {visible.length > 0 ? (
                    <div className='ml-model-grid'>
                      {visible.map((model) => (
                        <ModelCard key={model.model} model={model} />
                      ))}
                    </div>
                  ) : (
                    <EmptyState filtered={activeCount > 0} onReset={resetFilters} />
                  )}

                  <p className='ml-grid-note'>{t('models.gridNote')}</p>
                </div>
              </div>
            </>
          ) : null}
        </div>
      </main>

      <footer className='ml-foot'>
        <div className='ml-container ml-foot-inner'>
          <span>© 2026 MedRouter</span>
          <span className='ml-mono'>{t('models.footer.source')}</span>
        </div>
      </footer>
    </div>
  )
}
