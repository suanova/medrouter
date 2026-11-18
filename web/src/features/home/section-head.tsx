import type { ReactNode } from 'react'

interface SectionHeadProps {
  kicker?: string
  title: string
  lead?: string
  action?: ReactNode
}

export function SectionHead({ kicker, title, lead, action }: SectionHeadProps) {
  return (
    <div className='hm-section-head'>
      <div>
        {kicker ? <p className='hm-kicker'>{kicker}</p> : null}
        <h2 className='hm-section-title'>{title}</h2>
        {lead ? <p className='hm-section-lead'>{lead}</p> : null}
      </div>
      {action ? <div className='hm-section-action'>{action}</div> : null}
    </div>
  )
}
