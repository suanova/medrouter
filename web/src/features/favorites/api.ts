import { api, type ApiEnvelope } from '@/lib/api'

export type FavoriteKind = 'model' | 'dataset'

export interface Favorite {
  kind: FavoriteKind
  name: string
  created_at: string
}

function unwrap<T>(envelope: ApiEnvelope<T>): T {
  if (!envelope.success || envelope.data === undefined) {
    throw new Error(envelope.message || 'Request failed')
  }
  return envelope.data
}

export async function fetchFavorites(): Promise<Favorite[]> {
  const response = await api.get<ApiEnvelope<{ favorites: Favorite[] }>>('/studio/favorites')
  return unwrap(response.data).favorites ?? []
}

export async function addFavorite(kind: FavoriteKind, name: string): Promise<Favorite> {
  const response = await api.post<ApiEnvelope<Favorite>>('/studio/favorites', { kind, name })
  return unwrap(response.data)
}

export async function removeFavorite(kind: FavoriteKind, name: string): Promise<void> {
  await api.delete(
    `/studio/favorites/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`
  )
}
