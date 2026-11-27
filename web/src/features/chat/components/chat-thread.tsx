import { Download, Loader2, Paperclip, Wrench } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { artifactUrl } from '../api'
import type { ChatArtifact, ChatConversation, ChatTurn } from '../types'

interface ChatThreadProps {
  conversation: ChatConversation | null
  enabled: boolean
}

export function ChatThread({ conversation, enabled }: ChatThreadProps) {
  const { t } = useTranslation()
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const turns = conversation?.turns ?? []
  const last = turns[turns.length - 1]
  const activity = `${turns.length}:${last?.reply.length ?? 0}:${last?.state ?? ''}`

  useEffect(() => {
    const node = scrollRef.current
    if (node) {
      node.scrollTop = node.scrollHeight
    }
  }, [activity])

  if (!conversation || turns.length === 0) {
    return (
      <div className='chat-thread is-empty' ref={scrollRef}>
        <div className='chat-empty'>
          <Wrench aria-hidden='true' className='chat-empty-icon' />
          <strong>{enabled ? t('chat.emptyTitle') : t('chat.disabledTitle')}</strong>
          <p>{enabled ? t('chat.emptyText') : t('chat.disabledText')}</p>
        </div>
      </div>
    )
  }

  return (
    <div className='chat-thread' ref={scrollRef} aria-live='polite'>
      {turns.map((turn) => (
        <Turn key={turn.id} turn={turn} />
      ))}
    </div>
  )
}

function Turn({ turn }: { turn: ChatTurn }) {
  const { t } = useTranslation()
  return (
    <article className='chat-turn'>
      <div className='chat-msg chat-msg-user'>
        <p className='chat-msg-text'>{turn.prompt}</p>
        {turn.attachments.length > 0 ? (
          <ul className='chat-attachment-chips'>
            {turn.attachments.map((attachment) => (
              <li key={attachment.path}>
                <Paperclip aria-hidden='true' />
                {attachment.name}
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      <div className={`chat-msg chat-msg-agent is-${turn.state}`}>
        <div className='chat-agent-head'>
          <span className='chat-agent-badge'>{t('chat.agent')}</span>
          <TurnStatus turn={turn} />
        </div>
        {turn.reply ? <p className='chat-reply'>{turn.reply}</p> : null}
        {turn.state === 'running' && !turn.reply ? (
          <p className='chat-waiting'>
            <Loader2 aria-hidden='true' className='chat-spinner' />
            {t('chat.working')}
          </p>
        ) : null}
        {turn.error ? <p className='chat-error' role='alert'>{turn.error}</p> : null}
        {turn.toolCalls.length > 0 ? <ToolCalls calls={turn.toolCalls} /> : null}
        {turn.artifacts.length > 0 && turn.taskId ? (
          <Artifacts taskId={turn.taskId} artifacts={turn.artifacts} />
        ) : null}
      </div>
    </article>
  )
}

function TurnStatus({ turn }: { turn: ChatTurn }) {
  const { t } = useTranslation()
  if (turn.state === 'running') {
    return <Elapsed startedAt={turn.startedAt} rawStatus={turn.rawStatus} label={t('chat.running')} />
  }
  if (turn.state === 'canceled') {
    return <span className='chat-status is-canceled'>{t('chat.canceled')}</span>
  }
  if (turn.state === 'failed') {
    return <span className='chat-status is-failed'>{t('chat.failed')}</span>
  }
  const seconds = turn.finishedAt
    ? Math.max(1, Math.round((turn.finishedAt - turn.startedAt) / 1000))
    : null
  return (
    <span className='chat-status is-done'>
      {seconds ? t('chat.doneIn', { seconds }) : t('chat.done')}
    </span>
  )
}

function Elapsed({
  startedAt,
  rawStatus,
  label,
}: {
  startedAt: number
  rawStatus?: string
  label: string
}) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const seconds = Math.max(0, Math.round((now - startedAt) / 1000))
  const text =
    rawStatus === 'cancel-requested'
      ? `${label} · ${t('chat.stopping')}`
      : `${label} · ${formatDuration(seconds)}`
  return <span className='chat-status is-running'>{text}</span>
}

function formatDuration(seconds: number): string {
  if (seconds < 60) {
    return `${seconds}s`
  }
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${minutes}m ${String(rest).padStart(2, '0')}s`
}

function ToolCalls({ calls }: { calls: unknown[] }) {
  const { t } = useTranslation()
  const text = JSON.stringify(calls, null, 2) ?? ''
  return (
    <details className='chat-tools'>
      <summary>
        <Wrench aria-hidden='true' />
        {t('chat.toolCalls', { count: calls.length })}
      </summary>
      <pre>{text.length > 8000 ? `${text.slice(0, 8000)}…` : text}</pre>
    </details>
  )
}

function Artifacts({ taskId, artifacts }: { taskId: string; artifacts: ChatArtifact[] }) {
  const { t } = useTranslation()
  return (
    <div className='chat-artifacts'>
      <span className='chat-artifacts-title'>{t('chat.artifacts')}</span>
      <ul>
        {artifacts.map((artifact) => (
          <li key={artifact.key}>
            <a
              className='chat-artifact'
              href={artifactUrl(taskId, artifact.key)}
              download={artifact.name || artifact.key}
            >
              <Download aria-hidden='true' />
              <span>{artifact.name || artifact.key}</span>
              {artifact.mimeType ? <em>{artifact.mimeType}</em> : null}
            </a>
          </li>
        ))}
      </ul>
    </div>
  )
}
