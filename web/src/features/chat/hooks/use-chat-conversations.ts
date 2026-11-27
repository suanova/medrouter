import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { loadConversations, newChatId, saveConversations, trimConversations } from '../lib/conversations'
import type { ChatConversation } from '../types'

/**
 * Conversation history lives in localStorage, keyed by user id, so one browser
 * can hold separate histories for separate accounts and the server keeps no
 * chat transcript.
 */
export function useChatConversations(
  userId: number | null,
  defaultPreset: string,
  defaultModel: string
) {
  const [conversations, setConversations] = useState<ChatConversation[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const loadedFor = useRef<number | null>(null)

  useEffect(() => {
    if (userId === null || userId <= 0) {
      loadedFor.current = null
      setConversations([])
      setActiveId(null)
      return
    }
    const loaded = loadConversations(userId)
    loadedFor.current = userId
    setConversations(loaded)
    setActiveId(loaded[0]?.id ?? null)
  }, [userId])

  useEffect(() => {
    // Only persist after the load effect for this user has run; otherwise the
    // previous account's list would be written under the new key.
    if (userId !== null && loadedFor.current === userId) {
      saveConversations(userId, trimConversations(conversations))
    }
  }, [conversations, userId])

  const activeConversation = useMemo(
    () => conversations.find((conversation) => conversation.id === activeId) ?? null,
    [conversations, activeId]
  )

  const createConversation = useCallback(
    (options?: { preset?: string; model?: string }): ChatConversation => {
      const now = Date.now()
      const conversation: ChatConversation = {
        id: newChatId('conv'),
        title: '',
        preset: options?.preset ?? defaultPreset,
        model: options?.model ?? defaultModel,
        sessionId: '',
        turns: [],
        createdAt: now,
        updatedAt: now,
      }
      setConversations((current) => [conversation, ...current])
      setActiveId(conversation.id)
      return conversation
    },
    [defaultPreset, defaultModel]
  )

  const updateConversation = useCallback(
    (conversationId: string, update: (conversation: ChatConversation) => ChatConversation) => {
      setConversations((current) =>
        current.map((conversation) =>
          conversation.id === conversationId ? update(conversation) : conversation
        )
      )
    },
    []
  )

  const removeConversation = useCallback(
    (conversationId: string) => {
      setConversations((current) => {
        const next = current.filter((conversation) => conversation.id !== conversationId)
        setActiveId((selected) =>
          selected === conversationId ? (next[0]?.id ?? null) : selected
        )
        return next
      })
    },
    []
  )

  return {
    conversations,
    activeConversation,
    activeId,
    selectConversation: setActiveId,
    createConversation,
    updateConversation,
    removeConversation,
  }
}
