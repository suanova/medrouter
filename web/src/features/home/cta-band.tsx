import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

export function CtaBand() {
  const { t } = useTranslation()

  return (
    <section className='hm-cta'>
      <div className='hm-container hm-cta-grid'>
        <div className='hm-cta-card is-primary'>
          <p className='hm-kicker'>{t('home.cta.kicker')}</p>
          <h2>{t('home.cta.hospital.title')}</h2>
          <p>{t('home.cta.hospital.text')}</p>
          <Link to='/sign-up' className='hm-btn hm-btn-on-dark'>
            {t('home.cta.hospital.button')}
          </Link>
        </div>
        <div className='hm-cta-card'>
          <p className='hm-kicker'>{t('home.cta.kicker')}</p>
          <h2>{t('home.cta.vendor.title')}</h2>
          <p>{t('home.cta.vendor.text')}</p>
          <Link to='/sign-up' className='hm-btn hm-btn-secondary'>
            {t('home.cta.vendor.button')}
          </Link>
        </div>
      </div>
    </section>
  )
}
