import {
  ArrowUp,
  ChevronDown,
  Loader2,
  Paperclip,
  Plus,
  Square,
  Wrench,
  X,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { apiErrorMessage } from '@/lib/api'

import { uploadAttachment } from '../api'
import type {
  ChatAttachment,
  ChatModel,
  ChatUploadSettings,
} from '../types'

interface PendingAttachment {
  id: string
  name: string
  state: 'uploading' | 'ready' | 'error'
  attachment?: ChatAttachment
  error?: string
}

interface ChatComposerProps {
  disabled: boolean
  sending: boolean
  canceling: boolean
  running: boolean
  uploads: ChatUploadSettings | null
  preset: string
  presetLocked: boolean
  onPresetChange: (preset: string) => void
  models: ChatModel[]
  model: string
  onModelChange: (model: string) => void
  maxPromptChars: number
  onSend: (prompt: string, attachments: ChatAttachment[]) => Promise<string | null>
  onCancel: () => void
}

const PRESETS = ['standard', 'ptc'] as const

/**
 * Floating composer modelled on the dsh Web UI: one rounded card with the
 * message box on top and a tool row underneath (+ attach, preset, model, send).
 */
export function ChatComposer({
  disabled,
  sending,
  canceling,
  running,
  uploads,
  preset,
  presetLocked,
  onPresetChange,
  models,
  model,
  onModelChange,
  maxPromptChars,
  onSend,
  onCancel,
}: ChatComposerProps) {
  const { t } = useTranslation()
  const [prompt, setPrompt] = useState('')
  const [pending, setPending] = useState<PendingAttachment[]>([])
  const [error, setError] = useState('')
  const fileInput = useRef<HTMLInputElement | null>(null)
  const textarea = useRef<HTMLTextAreaElement | null>(null)

  const uploading = pending.some((item) => item.state === 'uploading')
  const readyAttachments = pending
    .filter((item) => item.state === 'ready' && item.attachment)
    .map((item) => item.attachment as ChatAttachment)
  const overLimit = prompt.length > maxPromptChars
  const canSend =
    !disabled && !sending && !uploading && prompt.trim().length > 0 && !overLimit

  // Grow with the message up to a limit, then scroll.
  useEffect(() => {
    const node = textarea.current
    if (!node) {
      return
    }
    node.style.height = 'auto'
    node.style.height = `${Math.min(node.scrollHeight, 168)}px`
  }, [prompt])

  const addFiles = async (files: FileList) => {
    if (!uploads?.enabled) {
      return
    }
    setError('')
    const room = uploads.max_files - pending.length
    const selected = Array.from(files).slice(0, Math.max(0, room))
    if (Array.from(files).length > room) {
      setError(t('chat.tooManyFiles', { count: uploads.max_files }))
    }
    for (const file of selected) {
      if (file.size > uploads.max_bytes) {
        setError(t('chat.fileTooLarge', { mb: Math.floor(uploads.max_bytes / (1 << 20)) }))
        continue
      }
      const id = `${file.name}-${file.size}-${Math.random().toString(36).slice(2, 8)}`
      setPending((current) => [...current, { id, name: file.name, state: 'uploading' }])
      try {
        const attachment = await uploadAttachment(file)
        setPending((current) =>
          current.map((item) =>
            item.id === id ? { ...item, state: 'ready', attachment } : item
          )
        )
      } catch (uploadError) {
        const message = apiErrorMessage(uploadError) ?? t('chat.uploadFailed')
        setPending((current) =>
          current.map((item) =>
            item.id === id ? { ...item, state: 'error', error: message } : item
          )
        )
      }
    }
  }

  const submit = async () => {
    if (!canSend) {
      return
    }
    setError('')
    const message = prompt.trim()
    const failure = await onSend(message, readyAttachments)
    if (failure) {
      setError(failure)
      return
    }
    setPrompt('')
    setPending([])
  }

  return (
    <div className='chat-composer'>
      <div className='chat-composer-box'>
        {pending.length > 0 ? (
          <ul className='chat-pending'>
            {pending.map((item) => (
              <li key={item.id} className={`chat-pending-item is-${item.state}`}>
                <Paperclip aria-hidden='true' />
                <span>{item.name}</span>
                {item.state === 'uploading' ? (
                  <Loader2 aria-hidden='true' className='chat-spinner' />
                ) : null}
                {item.state === 'error' ? <em>{item.error}</em> : null}
                <button
                  type='button'
                  onClick={() =>
                    setPending((current) => current.filter((entry) => entry.id !== item.id))
                  }
                  aria-label={t('chat.removeAttachment', { name: item.name })}
                >
                  <X aria-hidden='true' />
                </button>
              </li>
            ))}
          </ul>
        ) : null}

        <textarea
          ref={textarea}
          className='chat-textarea'
          rows={1}
          value={prompt}
          placeholder={disabled ? t('chat.disabledPlaceholder') : t('chat.placeholder')}
          disabled={disabled}
          onChange={(event) => setPrompt(event.target.value)}
          onKeyDown={(event) => {
            // Enter sends, Shift+Enter breaks the line, and an IME composition
            // (Chinese/Japanese input) never sends: Enter there commits the
            // candidate, not the message.
            if (event.key !== 'Enter' || event.shiftKey) {
              return
            }
            if (event.nativeEvent.isComposing || event.keyCode === 229) {
              return
            }
            event.preventDefault()
            void submit()
          }}
        />

        <div className='chat-composer-row'>
          <div className='chat-composer-left'>
            <button
              type='button'
              className='chat-round-btn'
              onClick={() => fileInput.current?.click()}
              disabled={
                disabled || !uploads?.enabled || pending.length >= (uploads?.max_files ?? 0)
              }
              title={uploads?.enabled ? t('chat.attach') : t('chat.uploadsDisabled')}
              aria-label={t('chat.attach')}
            >
              <Plus aria-hidden='true' />
            </button>
            <input
              ref={fileInput}
              type='file'
              multiple
              hidden
              accept={uploads?.accept.join(',')}
              onChange={(event) => {
                if (event.target.files) {
                  void addFiles(event.target.files)
                }
                event.target.value = ''
              }}
            />
            <label
              className={`chat-pill${presetLocked ? ' is-locked' : ''}`}
              title={presetLocked ? t('chat.presetLocked') : t('chat.preset')}
            >
              <Wrench aria-hidden='true' />
              <select
                value={PRESETS.includes(preset as (typeof PRESETS)[number]) ? preset : 'standard'}
                onChange={(event) => onPresetChange(event.target.value)}
                disabled={disabled || running || presetLocked}
                aria-label={t('chat.preset')}
              >
                {PRESETS.map((value) => (
                  <option key={value} value={value}>
                    {t(`chat.presets.${value}`)}
                  </option>
                ))}
              </select>
              <ChevronDown aria-hidden='true' />
            </label>
          </div>

          <div className='chat-composer-right'>
            <label className='chat-model' title={t('chat.model')}>
              <select
                value={model || models[0]?.id || ''}
                onChange={(event) => onModelChange(event.target.value)}
                disabled={disabled || models.length === 0}
                aria-label={t('chat.model')}
              >
                {models.map((option) => (
                  <option key={option.id} value={option.id} title={option.name}>
                    {option.id}
                  </option>
                ))}
              </select>
              <ChevronDown aria-hidden='true' />
            </label>
            {running ? (
              <button
                type='button'
                className='chat-send-btn is-stop'
                onClick={onCancel}
                disabled={canceling}
                aria-label={t('chat.stop')}
                title={t('chat.stop')}
              >
                {canceling ? (
                  <Loader2 aria-hidden='true' className='chat-spinner' />
                ) : (
                  <Square aria-hidden='true' />
                )}
              </button>
            ) : (
              <button
                type='button'
                className='chat-send-btn'
                onClick={() => void submit()}
                disabled={!canSend}
                aria-label={t('chat.send')}
                title={t('chat.send')}
              >
                {sending ? (
                  <Loader2 aria-hidden='true' className='chat-spinner' />
                ) : (
                  <ArrowUp aria-hidden='true' />
                )}
              </button>
            )}
          </div>
        </div>

        {overLimit ? (
          <p className='chat-composer-error' role='alert'>
            {t('chat.promptTooLong', { max: maxPromptChars })}
          </p>
        ) : error ? (
          <p className='chat-composer-error' role='alert'>
            {error}
          </p>
        ) : null}
      </div>
    </div>
  )
}
