import { describe, expect, it } from 'vitest'

import {
  applyTaskToTurn,
  chatStorageKey,
  loadConversations,
  titleFromPrompt,
  trimConversations,
  turnStateFromTask,
} from './conversations'
import type { ChatConversation, ChatTask, ChatTurn } from '../types'

function turn(overrides: Partial<ChatTurn> = {}): ChatTurn {
  return {
    id: 'turn-1',
    prompt: 'build a deck',
    attachments: [],
    taskId: 'task-1',
    sessionId: 'sess-1',
    state: 'running',
    reply: '',
    toolCalls: [],
    artifacts: [],
    startedAt: 1_000,
    ...overrides,
  }
}

function task(overrides: Partial<ChatTask> = {}): ChatTask {
  return {
    taskId: 'task-1',
    sessionId: 'sess-1',
    status: 'completed',
    reply: 'here you go',
    ...overrides,
  }
}

describe('titleFromPrompt', () => {
  it('uses the first line and truncates long prompts', () => {
    expect(titleFromPrompt('  make a PPT\nand more')).toBe('make a PPT')
    const long = 'x'.repeat(80)
    const title = titleFromPrompt(long)
    expect(title.endsWith('…')).toBe(true)
    expect(title.length).toBeLessThanOrEqual(42)
  })

  it('returns an empty title for blank input', () => {
    expect(titleFromPrompt('   ')).toBe('')
  })
})

describe('turnStateFromTask', () => {
  it('maps every dsh status the runtime reports', () => {
    expect(turnStateFromTask(task({ status: 'running' }))).toBe('running')
    expect(turnStateFromTask(task({ status: 'completed' }))).toBe('done')
    // max-tokens still carries a usable reply and is settled as success.
    expect(turnStateFromTask(task({ status: 'max-tokens' }))).toBe('done')
    expect(turnStateFromTask(task({ status: 'canceled' }))).toBe('canceled')
    expect(turnStateFromTask(task({ status: 'failed' }))).toBe('failed')
    expect(turnStateFromTask(task({ status: 'timeout' }))).toBe('failed')
  })
})

describe('applyTaskToTurn', () => {
  it('carries reply, artifacts and the settled state onto the turn', () => {
    const updated = applyTaskToTurn(turn(), task({
      reply: 'done',
      artifacts: [{ key: 'report.pdf', type: 'file', name: 'report.pdf' }],
      finishedAt: 5_000,
    }))
    expect(updated.state).toBe('done')
    expect(updated.reply).toBe('done')
    expect(updated.artifacts).toHaveLength(1)
    expect(updated.finishedAt).toBe(5_000)
    expect(updated.error).toBeUndefined()
  })

  it('records the failure reason but keeps a partial reply', () => {
    const updated = applyTaskToTurn(turn(), task({
      status: 'timeout',
      reply: 'partial',
      error: { code: 'timeout', message: 'task timed out' },
    }))
    expect(updated.state).toBe('failed')
    expect(updated.reply).toBe('partial')
    expect(updated.error).toContain('task timed out')
  })

  it('does not erase data when a poll omits fields', () => {
    const existing = turn({ reply: 'kept', toolCalls: [{ name: 'write' }] })
    const updated = applyTaskToTurn(existing, task({ status: 'running', reply: undefined, toolCalls: undefined }))
    expect(updated.reply).toBe('kept')
    expect(updated.toolCalls).toEqual([{ name: 'write' }])
    expect(updated.finishedAt).toBeUndefined()
  })
})

describe('trimConversations', () => {
  it('caps the list and keeps the newest turns', () => {
    const conversations: ChatConversation[] = Array.from({ length: 60 }, (_, index) => ({
      id: `conv-${index}`,
      title: `c${index}`,
      preset: 'standard',
      model: 'biomistral-7b-u1-svc',
      sessionId: '',
      turns: Array.from({ length: 120 }, (_, turnIndex) => turn({ id: `t-${turnIndex}` })),
      createdAt: index,
      updatedAt: index,
    }))
    const trimmed = trimConversations(conversations)
    expect(trimmed).toHaveLength(50)
    expect(trimmed[0].turns).toHaveLength(100)
    expect(trimmed[0].turns[99].id).toBe('t-119')
  })
})

describe('loadConversations', () => {
  it('fails a turn that was still running without a task id', () => {
    // Deliberately no model: records written before the picker existed must
    // load with an empty model so the page can adopt the server default.
    const conversation = {
      id: 'conv-1',
      title: 'deck',
      preset: 'standard',
      sessionId: '',
      turns: [
        turn({ id: 'submitting', taskId: '', state: 'running' }),
        turn({ id: 'polling', taskId: 'task-2', state: 'running' }),
      ],
      createdAt: 1,
      updatedAt: 2,
    }
    window.localStorage.setItem(chatStorageKey(42), JSON.stringify([conversation]))

    const loaded = loadConversations(42)
    expect(loaded).toHaveLength(1)
    expect(loaded[0].model).toBe('')
    // A submit that never returned has nothing to poll.
    expect(loaded[0].turns[0].state).toBe('failed')
    // A submitted turn keeps running so the page resumes polling it.
    expect(loaded[0].turns[1].state).toBe('running')
  })

  it('ignores corrupt storage', () => {
    window.localStorage.setItem(chatStorageKey(7), '{not json')
    expect(loadConversations(7)).toEqual([])
  })
})
