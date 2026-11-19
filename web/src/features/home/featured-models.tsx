import { Link } from '@tanstack/react-router'
import { RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatRelativeTime } from '@/features/models/format'
import { modelDescription } from '@/features/models/labels'
import type { ModelSummary } from '@/features/models/types'

import { SectionHead } from './section-head'
import { deliveryMode, pickFeaturedModels } from './select'

interface FeaturedModelsProps {
  models: ModelSummary[]
  loading: boolean
  error: boolean
  onRetry: () => void
}

export function FeaturedModels({ models, loading, error, onRetry }: FeaturedModelsProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const featured = pickFeaturedModels(models, 3)

  if (!loading && !error && featured.length === 0) return null

  return (
    <section className='hm-section' id='models'>
      <div className='hm-container'>
        <SectionHead
          kicker={t('home.models.kicker')}
          title={t('home.models.title')}
          lead={t('home.models.lead')}
          action={
            <Link to='/models' className='hm-link'>
              {t('home.models.viewAll')} <span aria-hidden='true'>→</span>
            </Link>
          }
        />

        {error ? (
          <div className='hm-error'>
            <p>{t('home.models.empty')}</p>
            <button type='button' className='hm-btn hm-btn-secondary' onClick={onRetry}>
              <RotateCw aria-hidden='true' />
              {t('home.models.retry')}
            </button>
          </div>
        ) : null}

        {loading ? (
          <div className='hm-model-grid' aria-hidden='true'>
            {[0, 1, 2].map((index) => (
              <div className='hm-model-card is-placeholder' key={index}>
                <div className='hm-skeleton hm-skeleton-badges' />
                <div className='hm-skeleton hm-skeleton-title' />
                <div className='hm-skeleton hm-skeleton-line' />
                <div className='hm-skeleton hm-skeleton-line short' />
              </div>
            ))}
          </div>
        ) : null}

        {!loading && !error ? (
          <div className='hm-model-grid'>
            {featured.map((model) => (
              <Link
                key={model.model}
                to='/models/$name'
                params={{ name: model.model }}
                className='hm-model-card'
              >
                <div className='hm-badge-row'>
                  {model.evidence ? (
                    <span className='hm-badge hm-evidence' data-level={model.evidence}>
                      {model.evidence}
                    </span>
                  ) : null}
                  <span
                    className={
                      deliveryMode(model.distributions) === 'api'
                        ? 'hm-badge hm-badge-accent'
                        : 'hm-badge'
                    }
                  >
                    {t(`home.passport.${deliveryMode(model.distributions)}`)}
                  </span>
                </div>
                <h3 className='hm-model-name'>{model.display_name}</h3>
                <p className='hm-model-use'>{modelDescription(model, t)}</p>
                <p className='hm-model-meta'>
                  {[
                    model.departments?.[0] || model.department,
                    model.tasks?.[0] || model.task,
                    model.latest_version,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
                <div className='hm-model-foot'>
                  <span>{model.publisher || t('models.cards.unknownPublisher')}</span>
                  <span>{t('models.cards.versions', { count: model.version_count })}</span>
                  <span>
                    {t('models.cards.updated', {
                      time: formatRelativeTime(model.updated_at, locale),
                    })}
                  </span>
                </div>
              </Link>
            ))}
          </div>
        ) : null}
      </div>
    </section>
  )
}
