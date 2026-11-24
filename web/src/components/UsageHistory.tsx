import { useState } from 'react'
import type { HistoryPoint } from '../lib/servers.ts'
import { type MetricRange, useProjectMetrics, useServerMetrics } from '../lib/metrics.ts'
import { HistoryChart, RangePicker } from './HistoryChart.tsx'
import { UsageChart } from './UsageChart.tsx'

const card = 'rounded-lg border border-zinc-800 bg-zinc-900/50 p-4'

/** Uso del servidor: en vivo (últimos 30 min, cada 15 s) o historial guardado. */
export function ServerUsage({ serverId, live }: { serverId: string; live: HistoryPoint[] }) {
  const [range, setRange] = useState<'live' | MetricRange>('live')
  const q = useServerMetrics(serverId, range === 'live' ? '1h' : range)
  return (
    <section className={`mt-4 ${card}`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-medium">{range === 'live' ? 'Uso en los últimos 30 minutos' : 'Historial de uso'}</h2>
        <RangePicker live value={range} onChange={(v) => setRange(v as typeof range)} />
      </div>
      <div className="mt-3">
        {range === 'live' ? (
          <UsageChart points={live} />
        ) : q.isError ? (
          <p className="text-sm text-red-400">{q.error.message}</p>
        ) : (
          <HistoryChart points={q.data ?? []} now={q.dataUpdatedAt} range={range} series={['cpu', 'mem', 'disk']} note="Promedio por intervalo; el pico de CPU y RAM aparece al pasar el cursor." />
        )}
      </div>
    </section>
  )
}

/** Historial de consumo del contenedor de un proyecto. */
export function ProjectUsage({ projectId }: { projectId: string }) {
  const [range, setRange] = useState<MetricRange>('24h')
  const q = useProjectMetrics(projectId, range)
  return (
    <section className={card}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-medium">Consumo del contenedor</h2>
        <RangePicker value={range} onChange={(v) => setRange(v as MetricRange)} />
      </div>
      <div className="mt-3">
        {q.isError ? (
          <p className="text-sm text-red-400">{q.error.message}</p>
        ) : (
          <HistoryChart
            points={q.data ?? []}
            now={q.dataUpdatedAt}
            range={range}
            series={['cpu', 'mem']}
            note="CPU: 100 % = un núcleo completo. RAM: porcentaje del límite del contenedor (o de la RAM del servidor si no tiene). Se mide una vez por minuto."
          />
        )}
      </div>
    </section>
  )
}
