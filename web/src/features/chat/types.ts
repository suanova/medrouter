/** DSH agent types shared by the API client, storage and components. */

export type ChatTaskStatus =
  | 'running'
  | 'completed'
  | 'max-tokens'
  | 'canceled'
  | 'failed'
  | 'timeout'

export interface ChatArtifact {
  key: string
  type: string
  mimeType?: string
  name?: string
}

export interface ChatTaskError {
  code?: string
  message?: string
}

export interface ChatTask {
  taskId: string
  sessionId: string
  status: ChatTaskStatus | string
  continuation?: boolean
  createdAt?: number
  startedAt?: number | null
  finishedAt?: number | null
  preset?: string
  model?: string
  prompt?: string
  reply?: string
  transcript?: unknown[]
  toolCalls?: unknown[]
  artifacts?: ChatArtifact[]
  turnEnd?: string
  error?: ChatTaskError | null
}

export interface ChatAttachment {
  name: string
  path: string
  mimeType?: string
  size?: number
}

export interface ChatUploadSettings {
  enabled: boolean
  max_bytes: number
  max_files: number
  accept: string[]
}

export interface ChatModel {
  id: string
  name: string
}

export interface ChatStatus {
  enabled: boolean
  default_preset: string
  default_model: string
  models: ChatModel[]
  max_prompt_chars: number
  uploads: ChatUploadSettings
}

/** UI state of one turn. The raw dsh status is kept for diagnostics. */
export type ChatTurnState = 'running' | 'done' | 'failed' | 'canceled'

export interface ChatTurn {
  id: string
  prompt: string
  attachments: ChatAttachment[]
  taskId: string
  sessionId: string
  state: ChatTurnState
  rawStatus?: string
  reply: string
  toolCalls: unknown[]
  artifacts: ChatArtifact[]
  error?: string
  startedAt: number
  finishedAt?: number
}

export interface ChatConversation {
  id: string
  title: string
  preset: string
  model: string
  sessionId: string
  turns: ChatTurn[]
  createdAt: number
  updatedAt: number
}
