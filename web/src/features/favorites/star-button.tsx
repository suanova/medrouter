import { queryOptions, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Star } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useAuthStore } from '@/stores/auth-store'

import { addFavorite, fetchFavorites, removeFavorite, type FavoriteKind } from './api'

export const FAVORITES_QUERY_KEY = ['studio', 'favorites'] as const

export const favoritesQueryOptions = queryOptions({
  queryKey: FAVORITES_QUERY_KEY,
  queryFn: fetchFavorites,
  staleTime: 30_000,
  // Favorites are per user; callers gate this with enabled: Boolean(user).
  retry: 1,
})

/** The path to return to after signing in, without the site base path. */
function currentPath(): string {
  const base = (window.__BASE_PATH__ ?? '').replace(/\/+$/, '')
  const pathname = window.location.pathname
  if (base && pathname.startsWith(base)) {
    return pathname.slice(base.length) || '/'
  }
  return pathname || '/'
}

interface StarButtonProps {
  kind: FavoriteKind
  name: string
}

/**
 * Stars one catalog entry. This is the only way an entry enters the studio's
 * one-click list, so it lives on the detail pages rather than the list pages.
 */
export function StarButton({ kind, name }: StarButtonProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.user)

  const favorites = useQuery({ ...favoritesQueryOptions, enabled: Boolean(user) })
  const starred =
    favorites.data?.some((favorite) => favorite.kind === kind && favorite.name === name) ?? false

  const mutation = useMutation({
    mutationFn: async () => {
      if (starred) {
        await removeFavorite(kind, name)
      } else {
        await addFavorite(kind, name)
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: FAVORITES_QUERY_KEY })
      toast.success(starred ? t('favorites.removed', { name }) : t('favorites.added', { name }))
    },
    onError: () => toast.error(t('favorites.failed')),
  })

  const onClick = () => {
    if (!user) {
      void navigate({ to: '/sign-in', search: { redirect: currentPath() } })
      return
    }
    mutation.mutate()
  }

  return (
    <button
      type='button'
      className={starred ? 'ml-btn ml-btn-secondary is-starred' : 'ml-btn ml-btn-secondary'}
      aria-pressed={starred}
      disabled={mutation.isPending}
      onClick={onClick}
      title={starred ? t('favorites.remove') : t('favorites.add')}
    >
      <Star aria-hidden='true' fill={starred ? 'currentColor' : 'none'} />
      {starred ? t('favorites.starred') : t('favorites.star')}
    </button>
  )
}
