import { api, type ApiEnvelope } from '@/lib/api'
import { withApiBase } from '@/lib/base-path'

import type { ChatAttachment, ChatStatus, ChatTask } from './types'

function unwrap<T>(envelope: ApiEnvelope<T>): T {
  if (!envelope.success || envelope.data === undefined) {
    throw new Error(envelope.message || 'Request failed')
  }
  return envelope.data
}

export async function fetchChatStatus(): Promise<ChatStatus> {
  const response = await api.get<ApiEnvelope<ChatStatus>>('/chat/status')
  return unwrap(response.data)
}

export interface SubmitTurnInput {
  prompt: string
  sessionId?: string
  preset?: string
  model?: string
  attachments?: ChatAttachment[]
}

export async function submitTurn(input: SubmitTurnInput): Promise<ChatTask> {
  const response = await api.post<ApiEnvelope<{ task: ChatTask }>>('/chat/tasks', input)
  return unwrap(response.data).task
}

export async function fetchTurn(taskId: string): Promise<ChatTask> {
  const response = await api.get<ApiEnvelope<{ task: ChatTask }>>(
    `/chat/tasks/${encodeURIComponent(taskId)}`
  )
  return unwrap(response.data).task
}

export async function cancelTurn(taskId: string): Promise<void> {
  await api.delete(`/chat/tasks/${encodeURIComponent(taskId)}`)
}

export async function uploadAttachment(file: File): Promise<ChatAttachment> {
  const form = new FormData()
  form.append('file', file)
  // No explicit Content-Type: the browser must add the multipart boundary.
  const response = await api.post<ApiEnvelope<{ attachment: ChatAttachment }>>(
    '/chat/uploads',
    form
  )
  return unwrap(response.data).attachment
}

/**
 * Same-origin download URL. The browser follows it with the session cookie and
 * receives the filename through Content-Disposition.
 */
export function artifactUrl(taskId: string, key: string): string {
  return withApiBase(`/chat/tasks/${encodeURIComponent(taskId)}/files/${encodeURIComponent(key)}`)
}
