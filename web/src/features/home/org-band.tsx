import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

export function OrgBand() {
  const { t } = useTranslation()

  return (
    <section className='hm-orgs' id='orgs'>
      <div className='hm-container hm-orgs-inner'>
        <div>
          <p className='hm-kicker'>{t('home.orgs.kicker')}</p>
          <h2 className='hm-orgs-title'>{t('home.orgs.title')}</h2>
          <p className='hm-orgs-text'>{t('home.orgs.text')}</p>
        </div>
        <Link to='/sign-up' className='hm-btn hm-btn-secondary'>
          {t('home.orgs.contact')}
        </Link>
      </div>
    </section>
  )
}
