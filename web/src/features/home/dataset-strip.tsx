import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import type { DatasetSummary } from '@/features/datasets/types'
import { formatNumber, formatRelativeTime } from '@/features/models/format'

import { SectionHead } from './section-head'
import { datasetAccess, pickFeaturedDatasets, statementState } from './select'

interface DatasetStripProps {
  datasets: DatasetSummary[]
  loading: boolean
}

export function DatasetStrip({ datasets, loading }: DatasetStripProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const featured = pickFeaturedDatasets(datasets, 3)

  if (!loading && featured.length === 0) return null

  return (
    <section className='hm-section' id='datasets'>
      <div className='hm-container'>
        <SectionHead
          kicker={t('home.datasets.kicker')}
          title={t('home.datasets.title')}
          lead={t('home.datasets.lead')}
          action={
            <Link to='/datasets' className='hm-link'>
              {t('home.datasets.viewAll')} <span aria-hidden='true'>→</span>
            </Link>
          }
        />

        {loading ? (
          <div className='hm-dataset-grid' aria-hidden='true'>
            {[0, 1, 2].map((index) => (
              <div className='hm-dataset-card is-placeholder' key={index}>
                <div className='hm-skeleton hm-skeleton-badges' />
                <div className='hm-skeleton hm-skeleton-title' />
                <div className='hm-skeleton hm-skeleton-line' />
              </div>
            ))}
          </div>
        ) : (
          <div className='hm-dataset-grid'>
            {featured.map((dataset) => (
              <Link key={dataset.dataset} to='/datasets' className='hm-dataset-card'>
                <div className='hm-badge-row'>
                  <span className={`hm-badge hm-access is-${datasetAccess(dataset.access)}`}>
                    {t(`home.datasets.access.${datasetAccess(dataset.access)}`)}
                  </span>
                  <span
                    className='hm-badge'
                    title={dataset.deidentification || undefined}
                  >
                    {t(
                      `home.datasets.deidentification.${statementState(dataset.deidentification)}`
                    )}
                  </span>
                  <span className='hm-badge' title={dataset.ethics || undefined}>
                    {t(`home.datasets.ethics.${statementState(dataset.ethics)}`)}
                  </span>
                </div>
                <h3 className='hm-dataset-name'>{dataset.display_name}</h3>
                <p className='hm-dataset-meta'>
                  {[
                    dataset.format,
                    dataset.latest_version,
                    dataset.record_count
                      ? t('home.datasets.records', {
                          n: formatNumber(dataset.record_count, locale),
                        })
                      : undefined,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
                <div className='hm-dataset-foot'>
                  <span>
                    {dataset.publisher || t('home.datasets.unknownPublisher')}
                  </span>
                  <span>
                    {t('models.cards.updated', {
                      time: formatRelativeTime(dataset.updated_at, locale),
                    })}
                  </span>
                </div>
              </Link>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}
