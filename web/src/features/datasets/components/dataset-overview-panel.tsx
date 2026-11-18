import { useTranslation } from 'react-i18next'

import type { DatasetDetail, DatasetOverview } from '../types'

interface DatasetOverviewPanelProps {
  detail: DatasetDetail
}

function LongFormDatasetOverview({ overview }: { overview: DatasetOverview }) {
  const { t } = useTranslation()

  const listBlock = (title: string, items?: string[]) =>
    items?.length ? (
      <div>
        <span className='ml-b-label'>{title}</span>
        <ul className='ml-overview-list'>
          {items.map((it, i) => (
            <li key={i}>{it}</li>
          ))}
        </ul>
      </div>
    ) : null

  return (
    <div className='ml-overview-body'>
      {overview.description ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>
            {t('datasets.detail.overview.descriptionTitle')}
          </h3>
          <p className='ml-overview-para'>{overview.description}</p>
        </section>
      ) : null}

      {overview.intended_use ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>
            {t('datasets.detail.overview.intendedUseTitle')}
          </h3>
          <p className='ml-overview-para'>{overview.intended_use}</p>
        </section>
      ) : null}

      {overview.limitations?.length ||
      overview.contraindications?.length ||
      overview.out_of_scope?.length ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('datasets.detail.boundaryTitle')}</h3>
          <div className='ml-overview-grid'>
            {listBlock(
              t('datasets.detail.overview.limitationsTitle'),
              overview.limitations,
            )}
            {listBlock(
              t('datasets.detail.overview.contraindicationsTitle'),
              overview.contraindications,
            )}
            {listBlock(
              t('datasets.detail.overview.outOfScopeTitle'),
              overview.out_of_scope,
            )}
          </div>
        </section>
      ) : null}

      {overview.benchmarks?.length ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>
            {t('datasets.detail.overview.benchmarksTitle')}
          </h3>
          <table className='ml-bench-table'>
            <thead>
              <tr>
                <th>{t('datasets.detail.overview.colDataset')}</th>
                <th>{t('datasets.detail.overview.colMetric')}</th>
                <th>{t('datasets.detail.overview.colValue')}</th>
                <th>{t('datasets.detail.overview.colSplit')}</th>
                <th>{t('datasets.detail.overview.colSource')}</th>
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
          <h3 className='ml-sub-h'>
            {t('datasets.detail.overview.regulatoryTitle')}
          </h3>
          <p className='ml-overview-para'>
            {[overview.regulatory_status, overview.regulatory_note]
              .filter(Boolean)
              .join(' · ')}
          </p>
        </section>
      ) : null}

      {overview.disclaimer ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>
            {t('datasets.detail.overview.disclaimerTitle')}
          </h3>
          <p className='ml-overview-para'>{overview.disclaimer}</p>
        </section>
      ) : null}

      {overview.links && Object.keys(overview.links).length ? (
        <section className='ml-overview-sec'>
          <h3 className='ml-sub-h'>{t('datasets.detail.overview.linksTitle')}</h3>
          <ul className='ml-overview-links'>
            {Object.entries(overview.links).map(([k, v]) => (
              <li key={k}>
                <a
                  className='ml-overview-link'
                  href={v}
                  target='_blank'
                  rel='noreferrer'
                >
                  {k}
                  <span className='ml-overview-link-arrow' aria-hidden='true'>
                    ↗
                  </span>
                </a>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  )
}

export function DatasetOverviewPanel({ detail }: DatasetOverviewPanelProps) {
  const { t } = useTranslation()

  return (
    <section
      className='ml-panel'
      role='tabpanel'
      id='ds-panel-overview'
      aria-labelledby='ds-tab-overview'
    >
      <div className='ml-panel-head'>
        <h2>{t('datasets.detail.overviewTitle')}</h2>
        <span className='ml-meta'>
          {t('datasets.detail.providedBy', {
            publisher: detail.publisher || t('datasets.fields.notAnnotated'),
          })}
        </span>
      </div>

      <h3 className='ml-sub-h' style={{ marginTop: 0 }}>
        {t('datasets.detail.boundaryTitle')}
      </h3>
      <div className='ml-boundary-grid ds-boundary'>
        <div>
          <span className='ml-b-label'>{t('datasets.fields.access')}</span>
          <p>{detail.access || t('datasets.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('datasets.fields.deidentification')}</span>
          <p>{detail.deidentification || t('datasets.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('datasets.fields.ethics')}</span>
          <p>{detail.ethics || t('datasets.fields.notAnnotated')}</p>
        </div>
        <div>
          <span className='ml-b-label'>{t('datasets.fields.sourceRepo')}</span>
          {/^https?:\/\//i.test(detail.source_repo) ? (
            <p>
              <a
                href={detail.source_repo}
                target='_blank'
                rel='noreferrer'
                className='ml-inline-link'
              >
                {detail.source_repo}
              </a>
            </p>
          ) : (
            <p>{detail.source_repo || t('datasets.fields.notAnnotated')}</p>
          )}
        </div>
      </div>

      {detail.overview ? <LongFormDatasetOverview overview={detail.overview} /> : null}
    </section>
  )
}