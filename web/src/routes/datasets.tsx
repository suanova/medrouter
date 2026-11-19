import { createFileRoute, Outlet } from '@tanstack/react-router'

import '@/features/models/models.css'
import '@/features/datasets/datasets.css'
import '@/features/files/files.css'

export const Route = createFileRoute('/datasets')({
  component: DatasetsLayout,
})

function DatasetsLayout() {
  return <Outlet />
}
