import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Loader2 } from 'lucide-react'
import { useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { SiteHeader } from '@/components/site-header'
import { apiErrorMessage } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { fetchChatStatus } from './api'
import { ChatComposer } from './components/chat-composer'
import { ChatThread } from './components/chat-thread'
import { ConversationList } from './components/conversation-list'
import { useChatConversations } from './hooks/use-chat-conversations'
import { useChatPolling, useChatTask } from './hooks/use-chat-task'
import type { ChatAttachment } from './types'

export function ChatPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.user)

  const statusQuery = useQuery({
    queryKey: ['chat', 'status'],
    queryFn: fetchChatStatus,
    staleTime: 60_000,
  })
  const status = statusQuery.data
  const defaultPreset = status?.default_preset || 'standard'
  const models = status?.models ?? []
  const defaultModel = status?.default_model || models[0]?.id || 'deepseek-flash'

  const {
    conversations,
    activeConversation,
    activeId,
    selectConversation,
    createConversation,
    updateConversation,
    removeConversation,
  } = useChatConversations(user?.id ?? null, defaultPreset, defaultModel)
  const { sendPrompt, cancel, sending, canceling } = useChatTask(updateConversation)
  useChatPolling(activeConversation, updateConversation)

  const enabled = Boolean(status?.enabled)
  const running = Boolean(
    activeConversation?.turns.some((turn) => turn.state === 'running')
  )

  // Conversations saved before the model picker existed have no model; adopt
  // the server default once it is known so the picker and the submit agree.
  useEffect(() => {
    if (activeConversation && !activeConversation.model && defaultModel) {
      updateConversation(activeConversation.id, (conversation) => ({
        ...conversation,
        model: defaultModel,
      }))
    }
  }, [activeConversation, defaultModel, updateConversation])

  const handleCreate = useCallback(() => {
    // Do not stack empty conversations: pressing "new" on a blank one is a
    // no-op.
    if (activeConversation && activeConversation.turns.length === 0) {
      return
    }
    createConversation({ model: activeConversation?.model || defaultModel })
  }, [activeConversation, createConversation, defaultModel])

  const handleSend = useCallback(
    async (prompt: string, attachments: ChatAttachment[]): Promise<string | null> => {
      const conversation =
        activeConversation ?? createConversation({ model: defaultModel })
      return sendPrompt(conversation, prompt, attachments)
    },
    [activeConversation, createConversation, defaultModel, sendPrompt]
  )

  const handlePresetChange = useCallback(
    (preset: string) => {
      if (!activeConversation) {
        createConversation({ preset, model: defaultModel })
        return
      }
      updateConversation(activeConversation.id, (conversation) => ({ ...conversation, preset }))
    },
    [activeConversation, createConversation, defaultModel, updateConversation]
  )

  const handleModelChange = useCallback(
    (model: string) => {
      if (!activeConversation) {
        createConversation({ model })
        return
      }
      updateConversation(activeConversation.id, (conversation) => ({ ...conversation, model }))
    },
    [activeConversation, createConversation, updateConversation]
  )

  const handleCancel = useCallback(() => {
    if (activeConversation) {
      void cancel(activeConversation)
    }
  }, [activeConversation, cancel])

  return (
    <div className='ml-scope'>
      <SiteHeader />
      <main className='ml-main'>
        <div className='ml-container chat-container'>
          <header className='chat-page-head'>
            <div>
              <p className='chat-kicker'>DSH · DeepSeek Harness</p>
              <h1>{t('chat.title')}</h1>
              <p className='chat-lead'>{t('chat.lead')}</p>
            </div>
            <span className='chat-local-note'>{t('chat.localHistory')}</span>
          </header>

          {statusQuery.isError ? (
            <div className='chat-banner is-error' role='alert'>
              <AlertTriangle aria-hidden='true' />
              {apiErrorMessage(statusQuery.error) ?? t('chat.statusFailed')}
            </div>
          ) : null}
          {statusQuery.isPending ? (
            <div className='chat-banner'>
              <Loader2 aria-hidden='true' className='chat-spinner' />
              {t('chat.loading')}
            </div>
          ) : null}
          {status && !enabled ? (
            <div className='chat-banner is-warning' role='status'>
              <AlertTriangle aria-hidden='true' />
              {t('chat.disabledText')}
            </div>
          ) : null}

          <div className='chat-shell'>
            <ConversationList
              conversations={conversations}
              activeId={activeId}
              onSelect={selectConversation}
              onCreate={handleCreate}
              onDelete={removeConversation}
            />
            <section className='chat-panel' aria-label={t('chat.title')}>
              <div className='chat-panel-head'>
                <div>
                  <h2>{activeConversation?.title || t('chat.untitled')}</h2>
                  <p>{t('chat.panelNote')}</p>
                </div>
                {running ? <span className='chat-live'>{t('chat.running')}</span> : null}
              </div>
              <ChatThread conversation={activeConversation} enabled={enabled} />
              <ChatComposer
                disabled={!enabled || statusQuery.isPending}
                sending={sending}
                canceling={canceling}
                running={running}
                uploads={status?.uploads ?? null}
                preset={activeConversation?.preset ?? defaultPreset}
                presetLocked={Boolean(activeConversation && activeConversation.turns.length > 0)}
                onPresetChange={handlePresetChange}
                models={models}
                model={activeConversation?.model || defaultModel}
                onModelChange={handleModelChange}
                maxPromptChars={status?.max_prompt_chars ?? 100000}
                onSend={handleSend}
                onCancel={handleCancel}
              />
            </section>
          </div>
        </div>
      </main>
    </div>
  )
}
