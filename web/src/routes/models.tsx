import { createFileRoute, Outlet } from '@tanstack/react-router'

import '@/features/models/models.css'
import '@/features/files/files.css'

export const Route = createFileRoute('/models')({
  component: ModelsLayout,
})

function ModelsLayout() {
  return <Outlet />
}
