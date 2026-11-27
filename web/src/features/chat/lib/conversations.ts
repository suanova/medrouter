import type {
  ChatConversation,
  ChatTask,
  ChatTurn,
  ChatTurnState,
} from '../types'

const STORAGE_VERSION = 'v1'
const MAX_CONVERSATIONS = 50
const MAX_TURNS = 100

export function chatStorageKey(userId: number): string {
  return `medrouter.chat.${STORAGE_VERSION}.${userId}`
}

/**
 * Locally-unique id without Web Crypto: crypto.randomUUID is undefined on
 * plain-http origins (an IP:port deployment), and the page must not throw.
 */
export function newChatId(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

export function titleFromPrompt(prompt: string): string {
  const first = prompt.trim().split('\n')[0]?.trim() ?? ''
  if (!first) {
    return ''
  }
  return first.length > 42 ? `${first.slice(0, 41)}…` : first
}

export function loadConversations(userId: number): ChatConversation[] {
  try {
    const raw = window.localStorage.getItem(chatStorageKey(userId))
    if (!raw) {
      return []
    }
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) {
      return []
    }
    return parsed
      .filter(isConversation)
      .slice(0, MAX_CONVERSATIONS)
      .map(recoverInterruptedTurns)
  } catch {
    return []
  }
}

/**
 * A turn that was still running at reload with no task id never finished its
 * submit, so there is nothing to poll. Mark it failed instead of spinning
 * forever. Turns that do have a task id keep running; the page resumes polling.
 */
function recoverInterruptedTurns(conversation: ChatConversation): ChatConversation {
  return {
    ...conversation,
    // Records written before the model picker existed have no model; the page
    // falls back to the server default instead of sending an empty id.
    model: typeof conversation.model === 'string' ? conversation.model : '',
    turns: conversation.turns.map((turn) =>
      turn.state === 'running' && !turn.taskId
        ? { ...turn, state: 'failed' as const, finishedAt: turn.finishedAt ?? Date.now() }
        : turn
    ),
  }
}

export function saveConversations(userId: number, conversations: ChatConversation[]): void {
  try {
    window.localStorage.setItem(
      chatStorageKey(userId),
      JSON.stringify(conversations.slice(0, MAX_CONVERSATIONS))
    )
  } catch {
    // A full or disabled localStorage must not break the conversation; the
    // current session keeps working in memory.
  }
}

export function turnStateFromTask(task: ChatTask): ChatTurnState {
  switch (task.status) {
    case 'failed':
    case 'timeout':
      return 'failed'
    case 'canceled':
      return 'canceled'
    case 'completed':
    case 'max-tokens':
      return 'done'
    default:
      return 'running'
  }
}

function taskErrorText(task: ChatTask): string {
  if (task.error && (task.error.code || task.error.message)) {
    const code = task.error.code ? ` (${task.error.code})` : ''
    return `${task.error.message ?? 'The agent stopped'}${code}`
  }
  if (task.turnEnd && task.turnEnd !== task.status) {
    return task.turnEnd
  }
  if (task.status === 'timeout') {
    return 'The turn timed out'
  }
  return ''
}

/** Fold a polled task into the turn that submitted it. */
export function applyTaskToTurn(turn: ChatTurn, task: ChatTask): ChatTurn {
  const state = turnStateFromTask(task)
  return {
    ...turn,
    taskId: task.taskId || turn.taskId,
    sessionId: task.sessionId || turn.sessionId,
    rawStatus: task.status,
    state,
    reply: task.reply ?? turn.reply,
    // Older task records omit these; never replace data already rendered.
    toolCalls: Array.isArray(task.toolCalls) ? task.toolCalls : turn.toolCalls,
    artifacts: Array.isArray(task.artifacts) ? task.artifacts : turn.artifacts,
    error: state === 'failed' ? taskErrorText(task) : undefined,
    finishedAt: state === 'running' ? undefined : (task.finishedAt ?? Date.now()),
  }
}

export function updateConversation(
  conversations: ChatConversation[],
  conversationId: string,
  update: (conversation: ChatConversation) => ChatConversation
): ChatConversation[] {
  return conversations.map((conversation) =>
    conversation.id === conversationId ? update(conversation) : conversation
  )
}

export function updateTurn(
  conversation: ChatConversation,
  turnId: string,
  update: (turn: ChatTurn) => ChatTurn
): ChatConversation {
  return {
    ...conversation,
    turns: conversation.turns.map((turn) => (turn.id === turnId ? update(turn) : turn)),
    updatedAt: Date.now(),
  }
}

function isConversation(value: unknown): value is ChatConversation {
  if (!value || typeof value !== 'object') {
    return false
  }
  const candidate = value as Partial<ChatConversation>
  return (
    typeof candidate.id === 'string' &&
    typeof candidate.title === 'string' &&
    Array.isArray(candidate.turns)
  )
}

/**
 * Keep the newest turns only. A long conversation would otherwise exceed the
 * localStorage quota after a few sessions.
 */
export function trimConversations(conversations: ChatConversation[]): ChatConversation[] {
  return conversations
    .slice(0, MAX_CONVERSATIONS)
    .map((conversation) => ({
      ...conversation,
      turns: conversation.turns.slice(-MAX_TURNS),
    }))
}
