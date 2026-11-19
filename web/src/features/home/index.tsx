import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { SiteHeader } from '@/components/site-header'
import { datasetListQueryOptions } from '@/features/datasets/queries'
import { modelListQueryOptions } from '@/features/models/queries'

import { DatasetStrip } from './dataset-strip'
import { DeliverySteps } from './delivery-steps'
import { FeaturedModels } from './featured-models'
import { Hero } from './hero'
import { SceneGrid } from './scene-grid'
import './home.css'

export function HomePage() {
  const { t } = useTranslation()
  const modelsQuery = useQuery(modelListQueryOptions)
  const datasetsQuery = useQuery(datasetListQueryOptions)
  const models = modelsQuery.data?.models ?? []
  const datasets = datasetsQuery.data?.datasets ?? []

  return (
    <div className='hm-scope'>
      <SiteHeader />
      <main>
        <Hero />
        <SceneGrid models={models} />
        <FeaturedModels
          models={models}
          loading={modelsQuery.isPending}
          error={modelsQuery.isError}
          onRetry={() => void modelsQuery.refetch()}
        />
        <DatasetStrip datasets={datasets} loading={datasetsQuery.isPending} />
        <DeliverySteps />
      </main>

      <footer className='hm-foot'>
        <div className='hm-container hm-foot-inner'>
          <div className='hm-foot-brand'>
            <span className='hm-brand'>MedRouter</span>
            <p>{t('home.footer.tagline')}</p>
          </div>
          <nav className='hm-foot-links' aria-label={t('home.footer.linksLabel')}>
            <Link to='/models'>{t('home.footer.links.models')}</Link>
            <Link to='/datasets'>{t('home.footer.links.datasets')}</Link>
            <a href='#scenes'>{t('home.footer.links.scenes')}</a>
            <Link to='/privacy-policy'>{t('home.footer.links.privacy')}</Link>
            <Link to='/user-agreement'>{t('home.footer.links.terms')}</Link>
          </nav>
          <span className='hm-foot-source'>
            © 2026 MedRouter · {t('home.footer.source')}
          </span>
        </div>
      </footer>
    </div>
  )
}
