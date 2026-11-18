import { useTranslation } from 'react-i18next'

import type { FacetKey, FacetValue, Facets, ModelFilterState } from '../filters'
import { FACET_KEYS, selectedValues } from '../filters'

interface FilterSidebarProps {
  facets: Facets
  filters: ModelFilterState
  open: boolean
  onToggle: (key: FacetKey, value: string) => void
  onClear: (key: FacetKey) => void
  onReset: () => void
  onClose: () => void
}

const GROUPS: FacetKey[] = [...FACET_KEYS]

export function FilterSidebar({
  facets,
  filters,
  open,
  onToggle,
  onClear,
  onReset,
  onClose,
}: FilterSidebarProps) {
  const { t } = useTranslation()

  return (
    <aside
      className={open ? 'ml-filters is-open' : 'ml-filters'}
      aria-label={t('models.filters.title')}
    >
      <div className='ml-filter-drawer-head'>
        <span>{t('models.filters.title')}</span>
        <button type='button' onClick={onClose}>
          {t('models.filters.close')}
        </button>
      </div>

      <p className='ml-filter-hint'>{t('models.filters.hint')}</p>

      {GROUPS.map((key) => {
        const options: FacetValue[] = [...facets[key]]
        const selected = selectedValues(filters[key])
        // Keep a shared-link value visible even when the current catalog no
        // longer has it; its count is 0 and it can still be removed.
        for (const value of selected) {
          if (!options.some((option) => option.value === value)) {
            options.push({ value, count: 0 })
          }
        }
        if (options.length === 0) return null
        return (
          <div className='ml-filter-group' key={key}>
            <h2 className='ml-filter-title'>{t(`models.filters.groups.${key}`)}</h2>
            <div role='group' aria-label={t(`models.filters.groups.${key}`)}>
              <button
                type='button'
                className={selected.length === 0 ? 'ml-filter-opt is-active' : 'ml-filter-opt'}
                aria-pressed={selected.length === 0}
                onClick={() => onClear(key)}
              >
                {t('models.filters.all')}
              </button>
              {options.map((option) => {
                const active = selected.includes(option.value)
                return (
                  <button
                    key={option.value}
                    type='button'
                    className={active ? 'ml-filter-opt is-active' : 'ml-filter-opt'}
                    aria-pressed={active}
                    onClick={() => onToggle(key, option.value)}
                  >
                    {option.value}
                    <span className='ml-filter-count'>{option.count}</span>
                  </button>
                )
              })}
            </div>
          </div>
        )
      })}

      <button type='button' className='ml-filter-reset' onClick={onReset}>
        {t('models.filters.reset')}
      </button>
    </aside>
  )
}
