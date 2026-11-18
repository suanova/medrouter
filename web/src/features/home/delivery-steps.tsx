import { useTranslation } from 'react-i18next'

import { SectionHead } from './section-head'

const STEPS = ['select', 'verify', 'authorize', 'integrate'] as const

export function DeliverySteps() {
  const { t } = useTranslation()

  return (
    <section className='hm-section' id='delivery'>
      <div className='hm-container'>
        <SectionHead
          kicker={t('home.delivery.kicker')}
          title={t('home.delivery.title')}
          lead={t('home.delivery.lead')}
        />
        <ol className='hm-steps'>
          {STEPS.map((step, index) => (
            <li className='hm-step' key={step}>
              <span className='hm-step-num'>{String(index + 1).padStart(2, '0')}</span>
              <h3>{t(`home.delivery.steps.${step}.title`)}</h3>
              <p>{t(`home.delivery.steps.${step}.text`)}</p>
            </li>
          ))}
        </ol>
        <p className='hm-disclaimer'>{t('home.delivery.disclaimer')}</p>
      </div>
    </section>
  )
}
