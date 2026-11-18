import type { ModelFilterState } from '@/features/models/filters'

export type HomeSceneId = 'lung' | 'pathology' | 'chart' | 'admet'

export interface HomeScene {
  id: HomeSceneId
  // Scene cards are saved searches: the same filters open on /models, so the
  // count on the card always matches the catalog result.
  filters: ModelFilterState
}

export const HOME_SCENES: HomeScene[] = [
  {
    id: 'lung',
    filters: { department: ['影像', '呼吸'], task: ['检测', '分割'] },
  },
  {
    id: 'pathology',
    filters: { department: ['病理'], modality: ['病理WSI'] },
  },
  {
    id: 'chart',
    filters: { modality: ['EMR文本'], task: ['分类', '检测(NER)'] },
  },
  {
    id: 'admet',
    filters: { department: ['药学'], task: ['药物虚拟筛选'] },
  },
]
