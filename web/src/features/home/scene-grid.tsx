import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  FileText,
  FlaskConical,
  Microscope,
  Stethoscope,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { ModelSummary } from '@/features/models/types'

import { HOME_SCENES, type HomeSceneId } from './scenes'
import { SectionHead } from './section-head'
import { bestEvidence, modelsForScene } from './select'

const ICONS: Record<HomeSceneId, LucideIcon> = {
  lung: Stethoscope,
  pathology: Microscope,
  chart: FileText,
  admet: FlaskConical,
}

interface SceneGridProps {
  models: ModelSummary[]
}

export function SceneGrid({ models }: SceneGridProps) {
  const { t } = useTranslation()

  return (
    <section className='hm-section' id='scenes'>
      <div className='hm-container'>
        <SectionHead
          kicker={t('home.scenes.kicker')}
          title={t('home.scenes.title')}
          lead={t('home.scenes.lead')}
        />
        <div className='hm-scenes'>
          {HOME_SCENES.map((scene) => {
            const Icon = ICONS[scene.id]
            const matches = modelsForScene(models, scene.filters)
            const evidence = bestEvidence(matches)
            return (
              <Link
                key={scene.id}
                to='/models'
                search={scene.filters}
                className='hm-scene-card'
              >
                <span className='hm-scene-icon'>
                  <Icon aria-hidden='true' />
                </span>
                <h3 className='hm-scene-title'>
                  {t(`home.scenes.items.${scene.id}.title`)}
                </h3>
                <p className='hm-scene-desc'>
                  {t(`home.scenes.items.${scene.id}.description`)}
                </p>
                <div className='hm-scene-meta'>
                  {models.length > 0 ? (
                    <span>{t('home.scenes.models', { n: matches.length })}</span>
                  ) : null}
                  {evidence ? (
                    <span>{t('home.scenes.bestEvidence', { level: evidence })}</span>
                  ) : null}
                </div>
                <span className='hm-scene-enter'>
                  {t('home.scenes.enter')} <ArrowUpRight aria-hidden='true' />
                </span>
              </Link>
            )
          })}
        </div>
      </div>
    </section>
  )
}
