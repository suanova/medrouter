import { useTranslation } from 'react-i18next'

import { SectionHead } from './section-head'

export function Evaluations() {
  const { t } = useTranslation()

  return (
    <section className='hm-section' id='medtrust'>
      <div className='hm-container'>
        <SectionHead
          kicker={t('home.evaluations.kicker')}
          title={t('home.evaluations.title')}
          lead={t('home.evaluations.lead')}
        />
        <div className='hm-eval-note'>
          <p>{t('home.evaluations.levels')}</p>
          <p className='hm-eval-pending'>{t('home.evaluations.note')}</p>
        </div>
      </div>
    </section>
  )
}
