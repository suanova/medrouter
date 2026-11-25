import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  BookOpen,
  ExternalLink,
  Loader2,
  Play,
  Server,
  SlidersHorizontal,
  Square,
  Trash2,
  X,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SiteHeader } from '@/components/site-header'
import { datasetListQueryOptions } from '@/features/datasets/queries'
import { favoritesQueryOptions } from '@/features/favorites/star-button'
import { PageHead } from '@/features/models/components/page-head'
import { formatRelativeTime } from '@/features/models/format'
import { modelListQueryOptions } from '@/features/models/queries'
import { apiErrorMessage } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { FineTuneDialog } from './finetune-dialog'
import {
  createWorkspace,
  deleteWorkspace,
  fetchWorkspaces,
  setWorkspaceRunning,
  WORKSPACES_QUERY_KEY,
  type Workspace,
} from './workspaces-api'

type FavoriteKind = 'model' | 'dataset'

interface Favorite {
  kind: FavoriteKind
  name: string
  /** Joined from the catalog; empty when the entry is no longer listed. */
  meta?: string
}

export function StudioPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? 'zh' : 'en'
  const user = useAuthStore((state) => state.user)
  const queryClient = useQueryClient()
  const [plan, setPlan] = useState<Workspace | null>(null)
  const [finetuneModel, setFinetuneModel] = useState<string | null>(null)

  const favoritesQuery = useQuery({ ...favoritesQueryOptions, enabled: Boolean(user) })
  const workspacesQuery = useQuery({
    queryKey: WORKSPACES_QUERY_KEY,
    queryFn: fetchWorkspaces,
    enabled: Boolean(user),
    // Creating is asynchronous on the platform side; poll while signed in so a
    // workspace turns "running" without a manual refresh.
    refetchInterval: 5_000,
  })
  const models = useQuery(modelListQueryOptions)
  const datasets = useQuery(datasetListQueryOptions)

  // Favorites carry only kind+name; the card subtitle is joined from the
  // catalogs already loaded on this page.
  const favorites: Favorite[] = (favoritesQuery.data ?? []).map((favorite) => ({
    kind: favorite.kind,
    name: favorite.name,
    meta:
      favorite.kind === 'model'
        ? (models.data?.models.find((m) => m.model === favorite.name)?.latest_version ?? '')
        : (datasets.data?.datasets.find((d) => d.dataset === favorite.name)?.format ?? ''),
  }))

  const workspaces = workspacesQuery.data ?? []
  const running = workspaces.filter((workspace) => workspace.phase === 'running').length
  const notes = [
    t('studio.headNotes.running', { count: running }),
    t('studio.headNotes.favorites', { count: favorites.length }),
  ]

  const workspaceFor = (favorite: Favorite, kind: Workspace['kind']) =>
    workspaces.find(
      (workspace) =>
        workspace.kind === kind &&
        workspace.source_kind === favorite.kind &&
        workspace.source_name === favorite.name
    )

  const launchMutation = useMutation({
    mutationFn: (favorite: Favorite) => createWorkspace(favorite.kind, favorite.name),
    onSuccess: (workspace) => {
      if (workspace.dry_run) {
        setPlan(workspace)
        return
      }
      void queryClient.invalidateQueries({ queryKey: WORKSPACES_QUERY_KEY })
      toast.success(t('studio.workspace.created', { name: workspace.name }))
    },
    onError: (error) => toast.error(apiErrorMessage(error) ?? t('studio.workspace.actionFailed')),
  })

  const runningMutation = useMutation({
    mutationFn: ({ id, running: next }: { id: number; running: boolean }) =>
      setWorkspaceRunning(id, next),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: WORKSPACES_QUERY_KEY })
    },
    onError: (error) => toast.error(apiErrorMessage(error) ?? t('studio.workspace.actionFailed')),
  })

  const deleteMutation = useMutation({
    mutationFn: (workspace: Workspace) => deleteWorkspace(workspace.id),
    onSuccess: (_result, workspace) => {
      void queryClient.invalidateQueries({ queryKey: WORKSPACES_QUERY_KEY })
      toast.success(t('studio.workspace.deleted', { name: workspace.name }))
    },
    onError: (error) => toast.error(apiErrorMessage(error) ?? t('studio.workspace.actionFailed')),
  })

  const openWorkspace = (workspace: Workspace) => {
    if (!workspace.endpoint) {
      toast.info(t('studio.workspace.noEndpoint'))
      return
    }
    window.open(workspace.endpoint, '_blank', 'noopener')
  }

  return (
    <div className='ml-scope'>
      <SiteHeader />
      <main className='ml-main'>
        <div className='ml-container'>
          <PageHead
            kicker={t('studio.kicker')}
            title={t('studio.title')}
            lead={t('studio.lead')}
            notes={notes}
            actions={
              <>
                <Link to='/models' className='ml-btn ml-btn-secondary'>
                  {t('studio.actions.models')}
                </Link>
                <Link to='/datasets' className='ml-btn ml-btn-secondary'>
                  {t('studio.actions.datasets')}
                </Link>
              </>
            }
          />

          <section className='st-favorites' aria-labelledby='st-favorites-title'>
            <div className='st-section-head is-bordered'>
              <h2 id='st-favorites-title'>{t('studio.favorites.title')}</h2>
              <span className='st-count'>{t('studio.favorites.note')}</span>
            </div>

            {!user ? (
              <div className='st-empty'>
                <strong>{t('studio.favorites.signInTitle')}</strong>
                <span>{t('studio.favorites.signInText')}</span>
                <Link to='/sign-in' className='ml-btn ml-btn-primary'>
                  {t('studio.favorites.signIn')}
                </Link>
              </div>
            ) : favoritesQuery.isPending ? (
              <div className='st-work-grid' aria-busy='true'>
                {[0, 1, 2].map((row) => (
                  <div key={row} className='st-fav is-skeleton' aria-hidden='true'>
                    <span className='st-skel-line' />
                    <span className='st-skel-line is-short' />
                  </div>
                ))}
              </div>
            ) : favoritesQuery.isError ? (
              <div className='st-empty'>
                <strong>{t('studio.favorites.errorTitle')}</strong>
                <span>{t('studio.favorites.errorText')}</span>
                <button
                  type='button'
                  className='ml-btn ml-btn-secondary'
                  onClick={() => void favoritesQuery.refetch()}
                >
                  {t('studio.favorites.retry')}
                </button>
              </div>
            ) : favorites.length === 0 ? (
              <div className='st-empty'>
                <strong>{t('studio.favorites.emptyTitle')}</strong>
                <span>{t('studio.favorites.emptyText')}</span>
                <Link to='/models' className='ml-btn ml-btn-primary'>
                  {t('studio.favorites.browse')}
                </Link>
              </div>
            ) : (
              <div className='st-work-grid'>
                {favorites.map((favorite) => (
                  <FavoriteCard
                    key={`${favorite.kind}:${favorite.name}`}
                    favorite={favorite}
                    inference={workspaceFor(favorite, 'inference')}
                    finetune={workspaceFor(favorite, 'finetune')}
                    pending={launchMutation.isPending}
                    onLaunch={() => launchMutation.mutate(favorite)}
                    onOpen={openWorkspace}
                    onFinetune={() => setFinetuneModel(favorite.name)}
                  />
                ))}
              </div>
            )}
          </section>

          <section className='st-workspaces' aria-labelledby='st-workspaces-title'>
            <div className='st-section-head is-bordered'>
              <h2 id='st-workspaces-title'>{t('studio.workspace.title')}</h2>
              <span className='st-count'>{t('studio.workspace.note')}</span>
            </div>

            {!user || workspacesQuery.isPending ? null : workspacesQuery.isError ? (
              <div className='st-empty'>
                <strong>{t('studio.workspace.errorTitle')}</strong>
                <span>{t('studio.workspace.errorText')}</span>
                <button
                  type='button'
                  className='ml-btn ml-btn-secondary'
                  onClick={() => void workspacesQuery.refetch()}
                >
                  {t('studio.favorites.retry')}
                </button>
              </div>
            ) : workspaces.length === 0 ? (
              <div className='st-empty'>
                <strong>{t('studio.workspace.emptyTitle')}</strong>
                <span>{t('studio.workspace.emptyText')}</span>
              </div>
            ) : (
              <div className='st-work-grid'>
                {workspaces.map((workspace) => (
                  <WorkspaceCard
                    key={workspace.id}
                    workspace={workspace}
                    locale={locale}
                    pending={runningMutation.isPending || deleteMutation.isPending}
                    onToggle={() =>
                      runningMutation.mutate({
                        id: workspace.id,
                        running: workspace.phase !== 'running',
                      })
                    }
                    onDelete={() => deleteMutation.mutate(workspace)}
                    onOpen={() => openWorkspace(workspace)}
                  />
                ))}
              </div>
            )}
          </section>
        </div>
      </main>

      {plan ? <PlanDialog plan={plan} onClose={() => setPlan(null)} /> : null}
      {finetuneModel ? (
        <FineTuneDialog model={finetuneModel} onClose={() => setFinetuneModel(null)} />
      ) : null}

      <footer className='ml-foot'>
        <div className='ml-container ml-foot-inner'>
          <span>© 2026 MedRouter</span>
          <span className='ml-mono'>{t('studio.footer.source')}</span>
        </div>
      </footer>
    </div>
  )
}

/**
 * A favorite is the unit of intent: the user already chose this model or
 * dataset, so the only remaining decision is "start it". Everything else —
 * runtime, size, cards, the dataset volume — is matched by the backend.
 */
function FavoriteCard({
  favorite,
  inference,
  finetune,
  pending,
  onLaunch,
  onOpen,
  onFinetune,
}: {
  favorite: Favorite
  inference?: Workspace
  finetune?: Workspace
  pending: boolean
  onLaunch: () => void
  onOpen: (workspace: Workspace) => void
  onFinetune: () => void
}) {
  const { t } = useTranslation()
  const isModel = favorite.kind === 'model'
  const Icon = isModel ? Server : BookOpen
  const workspace = inference ?? finetune
  // The deploy button follows the deployment only; a running fine-tune must not
  // disable it.
  const deploying = inference?.phase === 'creating'
  const running = inference?.phase === 'running'

  return (
    <article className='st-fav'>
      <header className='st-fav-head'>
        <span className='st-fav-kind'>
          <Icon aria-hidden='true' />
          {t(`studio.favorites.kinds.${favorite.kind}`)}
        </span>
        {workspace ? (
          <span className={`st-work-phase is-${workspace.phase}`}>
            <i aria-hidden='true' />
            {t(`studio.workspace.phases.${workspace.phase}`)}
          </span>
        ) : null}
      </header>

      <h3 className='st-fav-name'>{favorite.name}</h3>
      {favorite.meta ? <p className='st-fav-meta'>{favorite.meta}</p> : null}

      <p className='st-fav-auto'>
        {workspace?.matched.cpu
          ? [workspace.matched.cpu + 'C', workspace.matched.memory].filter(Boolean).join(' · ')
          : isModel
            ? t('studio.favorites.autoMatchInference')
            : t('studio.favorites.autoMatchNotebook')}
      </p>

      <footer className='st-fav-foot'>
        {isModel && finetune ? (
          <button type='button' className='ml-btn ml-btn-secondary st-fav-btn' disabled>
            {finetune.phase === 'creating' || finetune.phase === 'running' || finetune.phase === 'merging' ? (
              <Loader2 className='st-spin' aria-hidden='true' />
            ) : null}
            {t(`studio.finetune.phases.${finetune.phase}`)}
          </button>
        ) : null}
        {isModel ? (
          <button type='button' className='ml-btn ml-btn-secondary st-fav-btn' onClick={onFinetune}>
            {t('studio.favorites.finetune')}
          </button>
        ) : null}
        {running && inference ? (
          <button
            type='button'
            className='ml-btn ml-btn-primary st-fav-btn'
            onClick={() => onOpen(inference)}
          >
            <ExternalLink aria-hidden='true' />
            {t('studio.workspace.open')}
          </button>
        ) : (
          <button
            type='button'
            className='ml-btn ml-btn-primary st-fav-btn'
            disabled={pending || deploying}
            onClick={onLaunch}
          >
            {pending || deploying ? (
              <Loader2 className='st-spin' aria-hidden='true' />
            ) : (
              <Play aria-hidden='true' />
            )}
            {deploying
              ? t('studio.favorites.deploying')
              : isModel
                ? t('studio.favorites.deploy')
                : t('studio.favorites.start')}
          </button>
        )}
      </footer>
    </article>
  )
}

function WorkspaceCard({
  workspace,
  locale,
  pending,
  onToggle,
  onDelete,
  onOpen,
}: {
  workspace: Workspace
  locale: string
  pending: boolean
  onToggle: () => void
  onDelete: () => void
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const Icon =
    workspace.kind === 'inference' ? Server : workspace.kind === 'finetune' ? SlidersHorizontal : BookOpen
  const isJob = workspace.kind === 'finetune' || workspace.kind === 'inference'
  const active = workspace.phase === 'running'
  const busy = workspace.phase === 'creating'

  return (
    <article className='st-work'>
      <header className='st-work-head'>
        <span className='st-work-kind'>
          <Icon aria-hidden='true' />
          {t(`studio.workspace.kinds.${workspace.kind}`)}
        </span>
        <span className={`st-work-phase is-${workspace.phase}`}>
          <i aria-hidden='true' />
          {t(`studio.workspace.phases.${workspace.phase}`)}
        </span>
      </header>

      <h3 className='st-work-name'>{workspace.name}</h3>
      <p className='st-work-meta'>
        {[workspace.source_name, workspace.matched.cpu ? workspace.matched.cpu + 'C' : '', workspace.matched.memory]
          .filter(Boolean)
          .join(' · ')}
      </p>
      {workspace.endpoint ? (
        <p className='st-work-endpoint' title={workspace.endpoint}>
          {workspace.endpoint}
        </p>
      ) : null}
      {workspace.reason ? <p className='st-work-reason'>{workspace.reason}</p> : null}

      <footer className='st-work-foot'>
        <span className='st-work-time'>
          {workspace.created_at ? formatRelativeTime(workspace.created_at, locale) : ''}
        </span>
        <span className='st-work-actions'>
          {workspace.kind === 'notebook' && active ? (
            <button type='button' className='ml-btn ml-btn-secondary st-work-btn' onClick={onOpen}>
              <ExternalLink aria-hidden='true' />
              {t('studio.workspace.open')}
            </button>
          ) : null}
          {isJob ? null : (
            <button
              type='button'
              className='ml-btn ml-btn-secondary st-work-btn'
              disabled={busy || pending}
              onClick={onToggle}
            >
              {busy ? (
                <Loader2 className='st-spin' aria-hidden='true' />
              ) : active ? (
                <Square aria-hidden='true' />
              ) : (
                <Play aria-hidden='true' />
              )}
              {active ? t('studio.workspace.stop') : t('studio.workspace.start')}
            </button>
          )}
          <button
            type='button'
            className='ml-btn ml-btn-secondary st-work-btn is-danger'
            disabled={pending}
            onClick={onDelete}
            aria-label={t('studio.workspace.delete')}
          >
            <Trash2 aria-hidden='true' />
          </button>
        </span>
      </footer>
    </article>
  )
}

/**
 * Dry-run plan: the exact resources the backend would create. This is how the
 * matching rules can be reviewed before write access is granted.
 */
function PlanDialog({ plan, onClose }: { plan: Workspace; onClose: () => void }) {
  const { t } = useTranslation()
  const rows: Array<[string, string]> = [
    [t('studio.plan.name'), plan.name],
    [t('studio.plan.namespace'), plan.namespace],
    [t('studio.plan.image'), plan.matched.image ?? '—'],
    [
      t('studio.plan.resources'),
      [plan.matched.cpu ? plan.matched.cpu + 'C' : '', plan.matched.memory]
        .filter(Boolean)
        .join(' · ') || '—',
    ],
    [t('studio.plan.storage'), plan.matched.storage ?? '—'],
    [t('studio.plan.volume'), plan.matched.volume ?? '—'],
    [t('studio.plan.dataset'), plan.matched.dataset_size ?? '—'],
    [
      t('studio.plan.idle'),
      plan.matched.idle_timeout_seconds
        ? String(Math.round(plan.matched.idle_timeout_seconds / 3600)) + 'h'
        : '—',
    ],
  ]

  return (
    <div
      className='st-dialog-backdrop'
      role='presentation'
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className='st-dialog' role='dialog' aria-modal='true' aria-label={t('studio.plan.title')}>
        <header className='st-dialog-head'>
          <div>
            <p className='st-dialog-kicker'>{t('studio.plan.kicker')}</p>
            <h2>{t('studio.plan.title')}</h2>
          </div>
          <button type='button' className='st-icon-btn' onClick={onClose} aria-label={t('studio.plan.close')}>
            <X aria-hidden='true' />
          </button>
        </header>
        <dl className='st-plan-grid'>
          {rows.map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
        <p className='st-plan-note'>{t('studio.plan.note')}</p>
        <footer className='st-dialog-foot'>
          <button type='button' className='ml-btn ml-btn-primary' onClick={onClose}>
            {t('studio.plan.close')}
          </button>
        </footer>
      </div>
    </div>
  )
}
