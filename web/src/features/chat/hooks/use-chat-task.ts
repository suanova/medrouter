import { useCallback, useEffect, useState } from 'react'

import { apiErrorMessage } from '@/lib/api'

import { cancelTurn, fetchTurn, submitTurn } from '../api'
import {
  applyTaskToTurn,
  newChatId,
  titleFromPrompt,
  updateTurn,
} from '../lib/conversations'
import type { ChatAttachment, ChatConversation, ChatTurn } from '../types'

const POLL_INTERVAL_MS = 2000
// A transient network failure should not kill a turn the runtime is still
// working on; give up only after this many consecutive failed polls.
const MAX_POLL_FAILURES = 5

type UpdateConversation = (
  conversationId: string,
  update: (conversation: ChatConversation) => ChatConversation
) => void

/** Submit, cancel and status polling for the active conversation. */
export function useChatTask(updateConversation: UpdateConversation) {
  const [sending, setSending] = useState(false)
  const [canceling, setCanceling] = useState(false)

  const sendPrompt = useCallback(
    async (
      conversation: ChatConversation,
      prompt: string,
      attachments: ChatAttachment[]
    ): Promise<string | null> => {
      const turn: ChatTurn = {
        id: newChatId('turn'),
        prompt,
        attachments,
        taskId: '',
        sessionId: conversation.sessionId,
        state: 'running',
        reply: '',
        toolCalls: [],
        artifacts: [],
        startedAt: Date.now(),
      }
      updateConversation(conversation.id, (current) => ({
        ...current,
        title: current.title || titleFromPrompt(prompt),
        turns: [...current.turns, turn],
        updatedAt: Date.now(),
      }))
      setSending(true)
      try {
        const task = await submitTurn({
          prompt,
          sessionId: conversation.sessionId || undefined,
          preset: conversation.preset || undefined,
          model: conversation.model || undefined,
          attachments: attachments.length > 0 ? attachments : undefined,
        })
        updateConversation(conversation.id, (current) => ({
          ...current,
          sessionId: task.sessionId || current.sessionId,
          turns: current.turns.map((item) =>
            item.id === turn.id
              ? {
                  ...item,
                  taskId: task.taskId,
                  sessionId: task.sessionId || item.sessionId,
                  rawStatus: task.status,
                }
              : item
          ),
          updatedAt: Date.now(),
        }))
        return null
      } catch (error) {
        const message = apiErrorMessage(error) ?? 'Could not start the turn'
        // A session the runtime no longer holds (pod restarted or scaled) must
        // be dropped, or every later message would fail with the same id.
        const expired = apiErrorCode(error) === 'SESSION_EXPIRED'
        updateConversation(conversation.id, (current) => ({
          ...updateTurn(current, turn.id, (item) => ({
            ...item,
            state: 'failed',
            error: message,
            finishedAt: Date.now(),
          })),
          sessionId: expired ? '' : current.sessionId,
        }))
        return message
      } finally {
        setSending(false)
      }
    },
    [updateConversation]
  )

  const cancel = useCallback(
    async (conversation: ChatConversation): Promise<string | null> => {
      const running = [...conversation.turns]
        .reverse()
        .find((turn) => turn.state === 'running' && turn.taskId)
      if (!running) {
        return null
      }
      setCanceling(true)
      try {
        await cancelTurn(running.taskId)
        // The task settles as canceled on a later poll; showing the request
        // immediately keeps the button from being pressed twice.
        updateConversation(conversation.id, (current) =>
          updateTurn(current, running.id, (turn) => ({ ...turn, rawStatus: 'cancel-requested' }))
        )
        return null
      } catch (error) {
        return apiErrorMessage(error) ?? 'Could not stop the turn'
      } finally {
        setCanceling(false)
      }
    },
    [updateConversation]
  )

  return { sendPrompt, cancel, sending, canceling }
}

/**
 * Poll every running turn until it settles. The effect is keyed by the set of
 * running turns, so once one finishes the timer restarts without it.
 */
export function useChatPolling(
  conversation: ChatConversation | null,
  updateConversation: UpdateConversation
): void {
  const runningKey =
    conversation?.turns
      .filter((turn) => turn.state === 'running' && turn.taskId)
      .map((turn) => `${turn.id}:${turn.taskId}`)
      .join('|') ?? ''

  useEffect(() => {
    if (!conversation || !runningKey) {
      return
    }
    const conversationId = conversation.id
    const running = conversation.turns.filter((turn) => turn.state === 'running' && turn.taskId)
    let stopped = false
    let timer: number | undefined
    const failures = new Map<string, number>()

    const tick = async () => {
      for (const turn of running) {
        try {
          const task = await fetchTurn(turn.taskId)
          if (stopped) {
            return
          }
          failures.delete(turn.id)
          updateConversation(conversationId, (current) =>
            updateTurn(current, turn.id, (item) => applyTaskToTurn(item, task))
          )
        } catch (error) {
          if (stopped) {
            return
          }
          const count = (failures.get(turn.id) ?? 0) + 1
          failures.set(turn.id, count)
          const notFound = isNotFound(error)
          if (!notFound && count < MAX_POLL_FAILURES) {
            continue
          }
          const message = apiErrorMessage(error) ?? 'Lost contact with the agent'
          updateConversation(conversationId, (current) => ({
            ...updateTurn(current, turn.id, (item) => ({
              ...item,
              state: 'failed',
              error: message,
              finishedAt: Date.now(),
            })),
            // The task lived on a pod that no longer knows it; continuation
            // would fail the same way, so the next message starts fresh.
            sessionId: notFound ? '' : current.sessionId,
          }))
        }
      }
      if (!stopped) {
        timer = window.setTimeout(tick, POLL_INTERVAL_MS)
      }
    }

    void tick()
    return () => {
      stopped = true
      if (timer !== undefined) {
        window.clearTimeout(timer)
      }
    }
    // Keyed by the running set, not the whole conversation: a poll that only
    // adds reply text must not restart the timer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversation?.id, runningKey, updateConversation])
}

function apiErrorCode(error: unknown): string | null {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const payload = (error as { response?: { data?: { code?: unknown } } }).response?.data
    if (payload && typeof payload.code === 'string') {
      return payload.code
    }
  }
  return null
}

function isNotFound(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'response' in error &&
    (error as { response?: { status?: number } }).response?.status === 404
  )
}
