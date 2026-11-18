import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { SectionHead } from './section-head'

const DIMENSIONS = [
  'effectiveness',
  'safety',
  'fairness',
  'robustness',
  'efficiency',
  'compliance',
  'human',
] as const

export function Evaluations() {
  const { t } = useTranslation()

  return (
    <section className='hm-section' id='medtrust'>
      <div className='hm-container'>
        <SectionHead
          kicker={t('home.evaluations.kicker')}
          title={t('home.evaluations.title')}
          lead={t('home.evaluations.lead')}
          action={
            <Link to='/sign-up' className='hm-btn hm-btn-primary'>
              {t('home.evaluations.apply')}
            </Link>
          }
        />
        <div className='hm-eval-grid'>
          <div className='hm-dimensions'>
            {DIMENSIONS.map((dimension) => (
              <span className='hm-dim' key={dimension}>
                {t(`home.evaluations.dimensions.${dimension}`)}
              </span>
            ))}
          </div>
          <div className='hm-eval-note'>
            <p>{t('home.evaluations.levels')}</p>
            <p className='hm-eval-pending'>{t('home.evaluations.note')}</p>
          </div>
        </div>
      </div>
    </section>
  )
}
