import { useMutation, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, Loader2, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { apiErrorMessage } from '@/lib/api'

import { useFineTuneTargets } from './finetune-api'
import { createWorkspace, WORKSPACES_QUERY_KEY, type Workspace } from './workspaces-api'

interface FineTuneDialogProps {
  model: string
  onClose: () => void
}

/**
 * The only place in the studio where the user picks something, because a
 * fine-tune is inherently a model × dataset pair. The dataset list is filtered
 * by the backend's compatibility rules; incompatible entries stay visible
 * behind a toggle so a missing dataset never looks like a bug.
 */
export function FineTuneDialog({ model, onClose }: FineTuneDialogProps) {
  const { t } = useTranslation()
  const targets = useFineTuneTargets(model)
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState('')
  const [plan, setPlan] = useState<Workspace | null>(null)

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  useEffect(() => {
    if (!selected && targets.data?.datasets.length) {
      setSelected(targets.data.datasets[0].name)
    }
  }, [selected, targets.data])

  const start = useMutation({
    mutationFn: () => createWorkspace('model', model, selected),
    onSuccess: (workspace) => {
      if (workspace.dry_run) {
        setPlan(workspace)
        return
      }
      void queryClient.invalidateQueries({ queryKey: WORKSPACES_QUERY_KEY })
      toast.success(t('studio.workspace.created', { name: workspace.name }))
      onClose()
    },
    onError: (error) => toast.error(apiErrorMessage(error) ?? t('studio.workspace.actionFailed')),
  })

  return (
    <div
      className='st-dialog-backdrop'
      role='presentation'
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className='st-dialog' role='dialog' aria-modal='true' aria-label={t('studio.finetune.title')}>
        <header className='st-dialog-head'>
          <div>
            <p className='st-dialog-kicker'>{t('studio.finetune.kicker')}</p>
            <h2>{t('studio.finetune.title')}</h2>
            <p className='st-dialog-sub'>{t('studio.finetune.subtitle', { model })}</p>
          </div>
          <button type='button' className='st-icon-btn' onClick={onClose} aria-label={t('studio.finetune.close')}>
            <X aria-hidden='true' />
          </button>
        </header>

        {targets.isPending ? (
          <p className='st-plan-note'>{t('studio.finetune.loading')}</p>
        ) : targets.isError || !targets.data ? (
          <p className='st-plan-note'>{t('studio.finetune.loadFailed')}</p>
        ) : !targets.data.available ? (
          <div className='st-finetune-blocked'>
            <AlertCircle aria-hidden='true' />
            <div>
              <strong>{t('studio.finetune.unavailableTitle')}</strong>
              <span>{t(`studio.finetune.reasons.${targets.data.reason ?? 'default'}`)}</span>
            </div>
          </div>
        ) : targets.data.datasets.length === 0 ? (
          <div className='st-finetune-blocked'>
            <AlertCircle aria-hidden='true' />
            <div>
              <strong>{t('studio.finetune.noDatasetTitle')}</strong>
              <span>{t('studio.finetune.noDatasetText')}</span>
            </div>
          </div>
        ) : (
          <>
            <fieldset className='st-field'>
              <legend>{t('studio.finetune.dataset')}</legend>
              <div className='st-dataset-list'>
                {targets.data.datasets.map((dataset) => (
                  <label
                    key={dataset.name}
                    className={selected === dataset.name ? 'st-dataset is-active' : 'st-dataset'}
                  >
                    <input
                      type='radio'
                      name='finetune-dataset'
                      value={dataset.name}
                      checked={selected === dataset.name}
                      onChange={() => setSelected(dataset.name)}
                    />
                    <span className='st-dataset-main'>
                      <strong>{dataset.name}</strong>
                      <em>
                        {[dataset.format, dataset.size, t(`studio.finetune.tasks.${dataset.task}`)]
                          .filter(Boolean)
                          .join(' · ')}
                      </em>
                    </span>
                    {dataset.annotated ? null : (
                      <span className='st-dataset-hint'>{t('studio.finetune.inferred')}</span>
                    )}
                  </label>
                ))}
              </div>
            </fieldset>

            <dl className='st-plan-grid'>
              <div>
                <dt>{t('studio.finetune.method')}</dt>
                <dd>{t('studio.finetune.methodValue')}</dd>
              </div>
              <div>
                <dt>{t('studio.finetune.hardware')}</dt>
                <dd>{t('studio.finetune.hardwareValue')}</dd>
              </div>
              <div>
                <dt>{t('studio.finetune.recipe')}</dt>
                <dd>{t('studio.finetune.recipeValue')}</dd>
              </div>
              <div>
                <dt>{t('studio.finetune.tasksLabel')}</dt>
                <dd>{targets.data.tasks.map((task) => t(`studio.finetune.tasks.${task}`)).join(' / ')}</dd>
              </div>
            </dl>

            {plan ? (
              <>
                <p className='st-plan-note'>{t('studio.finetune.dryRunNote')}</p>
                <dl className='st-plan-grid'>
                  <div>
                    <dt>{t('studio.plan.name')}</dt>
                    <dd>{plan.name}</dd>
                  </div>
                  <div>
                    <dt>{t('studio.finetune.planOutput')}</dt>
                    <dd>{plan.matched.output ?? '—'}</dd>
                  </div>
                  <div>
                    <dt>{t('studio.finetune.planDeadline')}</dt>
                    <dd>
                      {plan.matched.deadline_seconds
                        ? String(Math.round(plan.matched.deadline_seconds / 3600)) + 'h'
                        : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t('studio.plan.image')}</dt>
                    <dd>{plan.matched.image ?? '—'}</dd>
                  </div>
                </dl>
              </>
            ) : (
              <p className='st-plan-note'>{t('studio.finetune.notReady')}</p>
            )}
          </>
        )}

        <footer className='st-dialog-foot'>
          <button type='button' className='ml-btn ml-btn-secondary' onClick={onClose}>
            {t('studio.finetune.close')}
          </button>
          <button
            type='button'
            className='ml-btn ml-btn-primary'
            disabled={!selected || start.isPending || Boolean(plan)}
            onClick={() => start.mutate()}
          >
            {start.isPending ? <Loader2 className='st-spin' aria-hidden='true' /> : null}
            {t('studio.finetune.submit')}
          </button>
        </footer>
      </div>
    </div>
  )
}
