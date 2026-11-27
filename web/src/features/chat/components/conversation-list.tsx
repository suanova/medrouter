import { MessageSquarePlus, Trash2 } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { ChatConversation } from '../types'

interface ConversationListProps {
  conversations: ChatConversation[]
  activeId: string | null
  onSelect: (id: string) => void
  onCreate: () => void
  onDelete: (id: string) => void
}

export function ConversationList({
  conversations,
  activeId,
  onSelect,
  onCreate,
  onDelete,
}: ConversationListProps) {
  const { t } = useTranslation()
  const ordered = useMemo(
    () => [...conversations].sort((left, right) => right.updatedAt - left.updatedAt),
    [conversations]
  )

  return (
    <aside className='chat-sidebar' aria-label={t('chat.conversations')}>
      <button type='button' className='ml-btn ml-btn-primary chat-new' onClick={onCreate}>
        <MessageSquarePlus aria-hidden='true' />
        {t('chat.newConversation')}
      </button>
      <ul className='chat-conversation-list'>
        {ordered.map((conversation) => (
          <li key={conversation.id}>
            <button
              type='button'
              className={`chat-conversation${conversation.id === activeId ? ' is-active' : ''}`}
              onClick={() => onSelect(conversation.id)}
              aria-current={conversation.id === activeId ? 'true' : undefined}
            >
              <span className='chat-conversation-title'>
                {conversation.title || t('chat.untitled')}
              </span>
              <span className='chat-conversation-meta'>
                {t('chat.turnCount', { count: conversation.turns.length })}
              </span>
            </button>
            <button
              type='button'
              className='chat-conversation-delete'
              onClick={() => onDelete(conversation.id)}
              aria-label={t('chat.deleteConversation')}
              title={t('chat.deleteConversation')}
            >
              <Trash2 aria-hidden='true' />
            </button>
          </li>
        ))}
      </ul>
      {ordered.length === 0 ? <p className='chat-sidebar-empty'>{t('chat.noConversations')}</p> : null}
    </aside>
  )
}
