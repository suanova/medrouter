import { useTranslation } from 'react-i18next'

import type { ModelDetail, ModelOverview } from '../types'

interface OverviewPanelProps {
  detail: ModelDetail
}

function LongFormOverview({ overview }: { overview: ModelOverview }) {
  const { t } = useTranslation()

  const listBlock = (
    title: string,
    items?: string[],
    fallback = ''
  ) =>
    items?.length ? (
      <div>
        <span className='ml-b-label'>{title}</span>
        <ul className='ml-overview-list'>
          {items.map((it, i) => (
            <li key={i}>{it}</li>
          ))}
        </ul>
      </div>
    ) : fallback ? (
      <span className='ml-meta'>{fallback}</span>
    ) : null

  return (
    <div className='ml-overview-body'>
      {overview.description ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.overview.descriptionTitle')}</h3>
          <p className='ml-overview-para'>{overview.description}</p>
        </section>
      ) : null}
      {overview.intended_use ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.overview.intendedUseTitle')}</h3>
          <p className='ml-overview-para'>{overview.intended_use}</p>
        </section>
      ) : null}

      {(overview.limitations?.length ||
        overview.contraindications?.length ||
        overview.out_of_scope?.length) ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.boundaryTitle')}</h3>
          <div className='ml-overview-grid'>
            {listBlock(
              t('models.detail.overview.limitationsTitle'),
              overview.limitations,
            )}
            {listBlock(
              t('models.detail.overview.contraindicationsTitle'),
              overview.contraindications,
            )}
            {listBlock(
              t('models.detail.overview.outOfScopeTitle'),
              overview.out_of_scope,
            )}
          </div>
        </section>
      ) : null}

      {overview.benchmarks?.length ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.overview.benchmarksTitle')}</h3>
          <table className='ml-bench-table'>
            <thead>
              <tr>
                <th>{t('models.detail.overview.colDataset')}</th>
                <th>{t('models.detail.overview.colMetric')}</th>
                <th>{t('models.detail.overview.colValue')}</th>
                <th>{t('models.detail.overview.colSplit')}</th>
                <th>{t('models.detail.overview.colSource')}</th>
              </tr>
            </thead>
            <tbody>
              {overview.benchmarks.map((b, i) => (
                <tr key={i}>
                  <td>{b.name}</td>
                  <td>{b.metric}</td>
                  <td>{b.value}</td>
                  <td>{b.split || ''}</td>
                  <td>{b.source || ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}

      {overview.regulatory_status || overview.regulatory_note ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.overview.regulatoryTitle')}</h3>
          <p className='ml-overview-para'>
            {[overview.regulatory_status, overview.regulatory_note]
              .filter(Boolean)
              .join(' · ')}
          </p>
        </section>
      ) : null}

      {overview.disclaimer ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.overview.disclaimerTitle')}</h3>
          <p className='ml-overview-para'>{overview.disclaimer}</p>
        </section>
      ) : null}

      {overview.links && Object.keys(overview.links).length ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('models.detail.overview.linksTitle')}</h3>
          <ul className='ml-overview-links'>
            {Object.entries(overview.links).map(([k, v]) => (
              <li key={k}>
                <a href={v} target='_blank' rel='noreferrer'>
                  {k}
                </a>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  )
}

export function OverviewPanel({ detail }: OverviewPanelProps) {
  const { t } = useTranslation()

  return (
    <section
      className='ml-panel'
      role='tabpanel'
      id='ml-panel-overview'
      aria-labelledby='ml-tab-overview'
    >
      <div className='ml-panel-head'>
        <h2>{t('models.detail.overviewTitle')}</h2>
        <span className='ml-meta'>
          {t('models.detail.providedBy', {
            publisher: detail.publisher || t('models.fields.notAnnotated'),
          })}
        </span>
      </div>

      <h3 className='ml-sub-h' style={{ marginTop: 0 }}>
        {t('models.detail.boundaryTitle')}
      </h3>
      <div className='ml-boundary-grid'>
        <div>
          <span className='ml-b-label'>{t('models.fields.department')}</span>
          <p>{detail.departments?.join('、') || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.modality')}</span>
          <p>{detail.modalities?.join('、') || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.task')}</span>
          <p>{detail.tasks?.join('、') || detail.task || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.evidence')}</span>
          <p>{detail.evidence || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.publisher')}</span>
          <p>{detail.publisher || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.distribution')}</span>
          <p>{detail.distributions?.join('、') || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.license')}</span>
          <p>{detail.license || t('models.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('models.fields.usage')}</span>
          <p>{detail.usage_boundary || t('models.fields.notAnnotated')}</p>
        </div>
      </div>

      {detail.overview ? (
        <LongFormOverview overview={detail.overview} />
      ) : null}
    </section>
  )
}
