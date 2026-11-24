import { Link } from 'react-router'
import { type Alert, KIND_LABELS, useAlerts } from '../lib/alerts.ts'
import { formatDateTime, formatRelative, formatSpan } from '../lib/format.ts'

/** Lista de alertas. Activas: ícono y etiqueta en rojo; resueltas: atenuadas. */
export function AlertsList({ alerts, showTarget = true }: { alerts: Alert[]; showTarget?: boolean }) {
  return (
    <ul className="divide-y divide-zinc-800/80">
      {alerts.map((a) => {
        const active = !a.resolvedAt
        const event = a.kind === 'deploy_failed'
        return (
          <li key={a.id} className="flex gap-3 py-2.5 text-sm">
            <span aria-hidden className={`mt-0.5 ${active ? 'text-[#d03b3b]' : event ? 'text-[#d03b3b]/70' : 'text-zinc-500'}`}>
              {active || event ? '✕' : '✓'}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-x-2">
                <span className={`font-medium ${active ? 'text-zinc-100' : 'text-zinc-300'}`}>{KIND_LABELS[a.kind]}</span>
                {showTarget && (
                  <Link to={`/${a.targetType === 'server' ? 'servers' : 'projects'}/${a.targetId}`} className="text-zinc-400 underline hover:text-zinc-200">
                    {a.targetName}
                  </Link>
                )}
                <span className="text-xs text-zinc-500">
                  {active
                    ? `activa desde ${formatRelative(a.startedAt)}`
                    : event
                      ? formatDateTime(a.startedAt)
                      : `${formatDateTime(a.startedAt)} · duró ${formatSpan(a.startedAt, a.resolvedAt)}`}
                </span>
              </div>
              <p className={`mt-0.5 break-words text-xs ${active ? 'text-zinc-300' : 'text-zinc-500'}`}>{a.message}</p>
            </div>
          </li>
        )
      })}
    </ul>
  )
}

/** Alertas activas de un servidor o proyecto (no muestra nada si no hay). */
export function TargetAlerts({ targetId }: { targetId: string }) {
  const { data } = useAlerts('open')
  const list = data?.filter((a) => a.targetId === targetId) ?? []
  if (list.length === 0) return null
  return (
    <section className="mt-6 rounded-lg border border-[#d03b3b]/50 bg-[#d03b3b]/10 px-4 py-1">
      <AlertsList alerts={list} showTarget={false} />
    </section>
  )
}
