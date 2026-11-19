import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowRight, Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatNumber, formatRelativeTime } from '@/features/models/format'
import { modelDescription } from '@/features/models/labels'
import type { ModelSummary } from '@/features/models/types'

import { HOME_SCENES } from './scenes'
import { deliveryMode, pickFeaturedModels } from './select'

interface HeroProps {
  models: ModelSummary[]
  modelTotal?: number
  datasetTotal?: number
}

export function Hero({ models, modelTotal, datasetTotal }: HeroProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const featured = pickFeaturedModels(models, 1)[0]

  const submitSearch = () => {
    const q = query.trim()
    void navigate({ to: '/models', search: q ? { q } : {} })
  }

  return (
    <section className='hm-hero'>
      <div className='hm-container hm-hero-grid'>
        <div className='hm-hero-copy'>
          <p className='hm-kicker'>{t('home.hero.kicker')}</p>
          <h1 className='hm-title'>{t('home.hero.title')}</h1>
          <p className='hm-lead'>{t('home.hero.lead')}</p>

          <form
            className='hm-search'
            role='search'
            onSubmit={(event) => {
              event.preventDefault()
              submitSearch()
            }}
          >
            <Search aria-hidden='true' />
            <input
              type='search'
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t('home.hero.searchPlaceholder')}
              aria-label={t('home.hero.searchPlaceholder')}
              autoComplete='off'
            />
            <button type='submit' className='hm-btn hm-btn-primary hm-search-btn'>
              {t('home.hero.searchButton')}
            </button>
          </form>

          <div className='hm-chips' aria-label={t('home.hero.chipsLabel')}>
            {HOME_SCENES.map((scene) => (
              <Link key={scene.id} to='/models' search={scene.filters} className='hm-chip'>
                {t(`home.hero.chips.${scene.id}`)}
              </Link>
            ))}
          </div>

          <div className='hm-cta-row'>
            <Link to='/models' className='hm-btn hm-btn-primary'>
              {t('home.hero.browse')} <ArrowRight aria-hidden='true' />
            </Link>
            <Link to='/sign-up' className='hm-btn hm-btn-secondary'>
              {t('home.hero.access')}
            </Link>
          </div>
        </div>

        <div className='hm-hero-side'>
          {featured ? (
            <Link
              to='/models/$name'
              params={{ name: featured.model }}
              className='hm-passport'
            >
              <div className='hm-badge-row'>
                {featured.evidence ? (
                  <span className='hm-badge hm-evidence' data-level={featured.evidence}>
                    {featured.evidence}
                  </span>
                ) : null}
                <span className='hm-badge'>{t('home.passport.regulatoryUnknown')}</span>
                <span
                  className={
                    deliveryMode(featured.distributions) === 'api'
                      ? 'hm-badge hm-badge-accent'
                      : 'hm-badge'
                  }
                >
                  {t(`home.passport.${deliveryMode(featured.distributions)}`)}
                </span>
              </div>
              <p className='hm-passport-kicker'>{t('home.passport.kicker')}</p>
              <h2 className='hm-passport-name'>{featured.display_name}</h2>
              <p className='hm-passport-use'>{modelDescription(featured, t)}</p>
              <p className='hm-passport-meta'>
                {[
                  featured.departments?.[0] || featured.department,
                  featured.modalities?.[0] || featured.modality,
                  featured.latest_version,
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </p>
              <p className='hm-passport-eval'>{t('home.passport.evaluationPending')}</p>
              <div className='hm-passport-foot'>
                <span>
                  {featured.publisher || t('models.cards.unknownPublisher')}
                </span>
                <span>
                  {t('models.cards.versions', { count: featured.version_count })}
                </span>
                <span>
                  {t('models.cards.updated', {
                    time: formatRelativeTime(featured.updated_at, locale),
                  })}
                </span>
              </div>
            </Link>
          ) : (
            <div className='hm-passport is-placeholder' aria-hidden='true'>
              <div className='hm-skeleton hm-skeleton-badges' />
              <div className='hm-skeleton hm-skeleton-title' />
              <div className='hm-skeleton hm-skeleton-line' />
              <div className='hm-skeleton hm-skeleton-line short' />
            </div>
          )}

          <div className='hm-trust'>
            {modelTotal !== undefined ? (
              <span>{t('home.trust.models', { n: formatNumber(modelTotal, locale) })}</span>
            ) : null}
            {datasetTotal !== undefined ? (
              <span>
                {t('home.trust.datasets', { n: formatNumber(datasetTotal, locale) })}
              </span>
            ) : null}
            <span>{t('home.trust.evidence')}</span>
            <span>{t('home.trust.delivery')}</span>
            <span>{t('home.trust.audit')}</span>
          </div>
        </div>
      </div>
    </section>
  )
}
