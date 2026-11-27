import { createFileRoute } from '@tanstack/react-router'

import '@/features/models/models.css'
import '@/features/chat/chat.css'
import { ChatPage } from '@/features/chat/chat-page'

export const Route = createFileRoute('/_authenticated/chat')({
  component: ChatPage,
})
