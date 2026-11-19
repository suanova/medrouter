import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export function Hero() {
  const { t, i18n } = useTranslation()
  const [activeScenario, setActiveScenario] = useState<
    'imaging' | 'records' | 'drug' | 'genomics'
  >('imaging')
  const language = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const scenario = (field: string) =>
    t(`home.hero.preview.samples.${activeScenario}.${field}`)

  return (
    <section className='hm-hero'>
      <svg
        className='hm-hero-science-art'
        viewBox='0 0 720 560'
        fill='none'
        aria-hidden='true'
        focusable='false'
      >
        <g className='hm-art-network'>
          <path d='M68 104 196 72 282 142 430 96 592 156 658 88' />
          <path d='M68 104 112 252 236 218 282 142 390 274 532 238 592 156 658 88' />
          <path d='M112 252 76 410 218 476 390 398 532 468 658 388' />
          <path d='M236 218 218 476M390 274 390 398M532 238 532 468M658 88 658 388' />
          <circle cx='68' cy='104' r='5' /><circle cx='196' cy='72' r='4' />
          <circle cx='282' cy='142' r='6' /><circle cx='430' cy='96' r='4' />
          <circle cx='592' cy='156' r='6' /><circle cx='658' cy='88' r='4' />
          <circle cx='112' cy='252' r='6' /><circle cx='236' cy='218' r='4' />
          <circle cx='390' cy='274' r='6' /><circle cx='532' cy='238' r='4' />
          <circle cx='76' cy='410' r='4' /><circle cx='218' cy='476' r='6' />
          <circle cx='390' cy='398' r='4' /><circle cx='532' cy='468' r='6' />
          <circle cx='658' cy='388' r='5' />
        </g>
        <g className='hm-art-helix'>
          <path d='M420 12C620 72 620 142 420 202S220 332 420 392 620 492 420 548' />
          <path d='M620 12C420 72 420 142 620 202S820 332 620 392 420 492 620 548' />
          <path d='M458 26 582 26M512 55 528 55M568 88 472 88M611 119 429 119M597 151 443 151M535 181 505 181M474 215 566 215M430 249 610 249M431 281 609 281M476 315 564 315M537 349 503 349M598 382 442 382M608 415 432 415M570 449 470 449M516 483 524 483M461 516 579 516' />
        </g>
      </svg>
      <div className='hm-container hm-hero-grid'>
        <div className='hm-hero-message'>
          <h1 className='hm-title' lang={language}>
            {t('home.hero.titlePrefix')}
            <span className='hm-title-mark'>{t('home.hero.titleMark')}</span>
          </h1>
          <p className='hm-hero-subtitle'>{t('home.hero.subtitle')}</p>
          <div className='hm-hero-actions'>
            <Link to='/sign-up' className='hm-hero-btn hm-hero-btn-primary'>
              {t('home.hero.primaryAction')}
              <ArrowRight aria-hidden='true' />
            </Link>
            <Link to='/models' className='hm-hero-btn hm-hero-btn-secondary'>
              {t('home.hero.secondaryAction')}
            </Link>
          </div>
        </div>

        <div
          className='hm-route-preview'
          role='region'
          aria-label={t('home.hero.preview.label')}
        >
          <div className='hm-route-topbar'>
            <span className='hm-route-glyph' aria-hidden='true'>
              <i />
              <i />
              <i />
              <i />
            </span>
            <span className='hm-route-brand'>MEDROUTER <b>/</b> ROUTER</span>
            <span className='hm-route-state'>{t('home.hero.preview.badge')}</span>
          </div>

          <div className='hm-route-body'>
            <p className='hm-route-label'>{t('home.hero.preview.requestLabel')}</p>
            <h2>{scenario('request')}</h2>
            <div className='hm-route-scenarios' role='group' aria-label={t('home.hero.preview.scenarioLabel')}>
              {(['imaging', 'records', 'drug', 'genomics'] as const).map((key) => (
                <button
                  className={activeScenario === key ? 'is-active' : ''}
                  type='button'
                  key={key}
                  aria-pressed={activeScenario === key}
                  onClick={() => setActiveScenario(key)}
                >
                  {t(`home.hero.preview.scenarios.${key}`)}
                </button>
              ))}
            </div>

            <div className='hm-route-trace'>
              <div className='hm-route-trace-step'>
                <span className='hm-route-step-index'>01</span>
                <div className='hm-route-step-content'>
                  <span className='hm-route-step-label'>{t('home.hero.preview.steps.parse')}</span>
                  <strong>{scenario('parse')}</strong>
                </div>
              </div>
              <div className='hm-route-trace-step'>
                <span className='hm-route-step-index'>02</span>
                <div className='hm-route-step-content'>
                  <span className='hm-route-step-label'>{t('home.hero.preview.steps.candidates')}</span>
                  <div className='hm-route-candidates'>
                    {[0, 1, 2].map((index) => (
                      <span className={index === 0 ? 'is-selected' : ''} key={index}>
                        <b>{scenario(`candidate${index + 1}Name`)}</b>
                        {scenario(`candidate${index + 1}Meta`)}
                      </span>
                    ))}
                  </div>
                </div>
              </div>
              <div className='hm-route-trace-step'>
                <span className='hm-route-step-index'>03</span>
                <div className='hm-route-step-content'>
                  <span className='hm-route-step-label'>{t('home.hero.preview.steps.select')}</span>
                  <strong>{scenario('selectedModel')}</strong>
                  <p>{scenario('reason')}</p>
                </div>
              </div>
            </div>

            <div className='hm-route-output'>
              <p className='hm-route-label'>{t('home.hero.preview.outputLabel')}</p>
              <p>{scenario('output')}</p>
              <div className='hm-route-output-tags'>
                <span className='is-warning'>{scenario('evidence')}</span>
                <span>{scenario('review')}</span>
              </div>
            </div>
          </div>

          <div className='hm-route-foot'>
            <span className='hm-route-foot-dot' aria-hidden='true' />
            {t('home.hero.preview.footnote')}
          </div>
        </div>
      </div>
    </section>
  )
}
