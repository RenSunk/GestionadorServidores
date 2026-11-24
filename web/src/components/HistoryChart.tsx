import { useEffect, useRef, useState } from 'react'
import { formatAxisTime, formatDateTime, formatPercent } from '../lib/format.ts'
import { type MetricPoint, type MetricRange, RANGES, expectedStep } from '../lib/metrics.ts'

// Todas las series en porcentaje: un solo eje. Colores: slots 1–3 de la paleta
// categórica validada (variante oscura), en orden fijo.
export type SeriesKey = 'cpu' | 'mem' | 'disk'
const ALL: Record<SeriesKey, { label: string; color: string; max?: 'cpuMax' | 'memMax' }> = {
  cpu: { label: 'CPU', color: '#3987e5', max: 'cpuMax' },
  mem: { label: 'RAM', color: '#d95926', max: 'memMax' },
  disk: { label: 'Disco', color: '#199e70' },
}

const HEIGHT = 220
const PAD = { top: 12, right: 84, bottom: 24, left: 40 }

type Props = {
  points: MetricPoint[]
  range: MetricRange
  series: SeriesKey[]
  /** Momento en que se obtuvieron los datos (extremo derecho del eje). */
  now: number
  /** Texto bajo el gráfico (ej. qué significa 100 % en un contenedor). */
  note?: string
}

export function HistoryChart({ points, range, series, now, note }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    if (!ref.current) return
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    ro.observe(ref.current)
    return () => ro.disconnect()
  }, [])

  const spanMs = RANGES.find((r) => r.key === range)!.ms
  const step = expectedStep(range)
  const to = now
  const from = to - spanMs
  const ts = points.map((p) => new Date(p.t).getTime())
  const val = (p: MetricPoint, k: SeriesKey) => (k === 'disk' ? p.disk : p[k])

  // Escala: 0–100 %, o más si un contenedor usa más de un núcleo (CPU > 100 %).
  const peak = Math.max(100, ...points.flatMap((p) => series.map((k) => val(p, k) ?? 0)))
  const yMax = peak <= 100 ? 100 : Math.ceil(peak / 100) * 100

  const innerW = Math.max(1, width - PAD.left - PAD.right)
  const innerH = HEIGHT - PAD.top - PAD.bottom
  const x = (ms: number) => PAD.left + ((ms - from) / spanMs) * innerW
  const y = (v: number) => PAD.top + innerH - (Math.min(yMax, Math.max(0, v)) / yMax) * innerH

  // Cortar la línea donde faltan puntos (servidor caído o instancia apagada).
  function path(k: SeriesKey) {
    let d = ''
    let prev: number | null = null
    points.forEach((p, i) => {
      const v = val(p, k)
      if (v == null) {
        prev = null
        return
      }
      const gap = prev === null || ts[i] - prev > step * 2.5
      d += `${gap ? 'M' : 'L'}${x(ts[i]).toFixed(1)},${y(v).toFixed(1)}`
      prev = ts[i]
    })
    return d
  }

  const legend = (
    <div className="flex gap-4 text-xs text-zinc-400">
      {series.map((k) => (
        <span key={k} className="inline-flex items-center gap-1.5">
          <span className="inline-block h-0.5 w-4 rounded" style={{ background: ALL[k].color }} />
          {ALL[k].label}
        </span>
      ))}
    </div>
  )

  if (points.length === 0) {
    return (
      <div ref={ref}>
        {legend}
        <div className="flex h-[220px] items-center justify-center text-center text-sm text-zinc-500">
          Sin datos en este rango. El historial se guarda por minuto desde que se agrega el servidor
          {range === '7d' || range === '30d' ? ' y se resume por hora (la primera hora completa aparece al terminar).' : '.'}
        </div>
      </div>
    )
  }

  const last = points[points.length - 1]
  const lastFresh = to - ts[ts.length - 1] < step * 3
  const grid = yMax === 100 ? [0, 50, 100] : [0, yMax / 2, yMax]

  function onMove(e: React.PointerEvent<SVGRectElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const ms = from + ((e.clientX - rect.left) / rect.width) * spanMs
    let best = 0
    for (let i = 1; i < ts.length; i++) if (Math.abs(ts[i] - ms) < Math.abs(ts[best] - ms)) best = i
    setHover(Math.abs(ts[best] - ms) < step * 3 ? best : null)
  }
  const hp = hover !== null ? points[hover] : null

  // Etiquetas directas con el último valor; se separan si quedan encimadas.
  const labels = series
    .map((k) => ({ k, v: val(last, k) }))
    .filter((l): l is { k: SeriesKey; v: number } => l.v != null)
    .map((l) => ({ ...l, py: y(l.v) }))
    .sort((a, b) => a.py - b.py)
  for (let i = 1; i < labels.length; i++) labels[i].py = Math.max(labels[i].py, labels[i - 1].py + 12)

  return (
    <div ref={ref} className="relative">
      {legend}
      <svg width={width} height={HEIGHT} className="mt-2 block" role="img" aria-label={`Uso de ${series.map((k) => ALL[k].label).join(', ')} en el tiempo`}>
        {grid.map((v) => (
          <g key={v}>
            <line x1={PAD.left} x2={PAD.left + innerW} y1={y(v)} y2={y(v)} stroke="#3f3f46" strokeWidth={1} strokeDasharray={v === 0 ? undefined : '2 4'} />
            <text x={PAD.left - 8} y={y(v)} dy="0.32em" textAnchor="end" className="fill-zinc-500 text-[10px]">
              {v}%
            </text>
          </g>
        ))}
        <text x={PAD.left} y={HEIGHT - 6} className="fill-zinc-500 text-[10px]">
          {formatAxisTime(from, spanMs)}
        </text>
        <text x={PAD.left + innerW} y={HEIGHT - 6} textAnchor="end" className="fill-zinc-500 text-[10px]">
          ahora
        </text>

        {series.map((k) => (
          <path key={k} d={path(k)} fill="none" stroke={ALL[k].color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        ))}

        {lastFresh &&
          labels.map((l) => (
            <text key={l.k} x={PAD.left + innerW + 6} y={l.py} dy="0.32em" className="fill-zinc-300 text-[10px] tabular-nums">
              {ALL[l.k].label} {formatPercent(l.v)}
            </text>
          ))}

        {hp && hover !== null && (
          <g pointerEvents="none">
            <line x1={x(ts[hover])} x2={x(ts[hover])} y1={PAD.top} y2={PAD.top + innerH} stroke="#71717a" strokeWidth={1} />
            {series.map((k) => {
              const v = val(hp, k)
              return v == null ? null : <circle key={k} cx={x(ts[hover])} cy={y(v)} r={4} fill={ALL[k].color} stroke="#18181b" strokeWidth={2} />
            })}
          </g>
        )}

        <rect x={PAD.left} y={PAD.top} width={innerW} height={innerH} fill="transparent" onPointerMove={onMove} onPointerLeave={() => setHover(null)} />
      </svg>

      {hp && hover !== null && (
        <div
          className="pointer-events-none absolute top-8 rounded-md border border-zinc-700 bg-zinc-900 px-2.5 py-1.5 text-xs shadow-lg"
          style={{ left: Math.min(x(ts[hover]) + 12, width - 170) }}
        >
          <div className="text-zinc-400">{formatDateTime(hp.t)}</div>
          {series.map((k) => {
            const v = val(hp, k)
            const mk = ALL[k].max
            if (v == null) return null
            return (
              <div key={k} className="flex items-center gap-2 tabular-nums text-zinc-100">
                <span className="inline-block h-0.5 w-3 rounded" style={{ background: ALL[k].color }} />
                {ALL[k].label} {formatPercent(v)}
                {mk && hp[mk] - v >= 1 && <span className="text-zinc-500">· pico {formatPercent(hp[mk])}</span>}
              </div>
            )
          })}
        </div>
      )}

      {note && <p className="mt-1 text-[11px] text-zinc-500">{note}</p>}

      <details className="mt-2 text-xs text-zinc-400">
        <summary className="cursor-pointer hover:text-zinc-200">Ver como tabla</summary>
        <table className="mt-2 w-full max-w-md tabular-nums">
          <thead className="text-left text-zinc-500">
            <tr>
              <th className="py-1 font-normal">Momento</th>
              {series.map((k) => (
                <th key={k} className="py-1 font-normal">
                  {ALL[k].label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="text-zinc-300">
            {points
              .slice(-12)
              .reverse()
              .map((p) => (
                <tr key={p.t}>
                  <td className="py-0.5">{formatDateTime(p.t)}</td>
                  {series.map((k) => {
                    const v = val(p, k)
                    const mk = ALL[k].max
                    return (
                      <td key={k} className="py-0.5">
                        {v == null ? '—' : formatPercent(v)}
                        {mk && <span className="text-zinc-500"> (pico {formatPercent(p[mk])})</span>}
                      </td>
                    )
                  })}
                </tr>
              ))}
          </tbody>
        </table>
      </details>
    </div>
  )
}

export function RangePicker({ value, onChange, live }: { value: string; onChange: (v: string) => void; live?: boolean }) {
  const opts = [...(live ? [{ key: 'live', label: 'En vivo' }] : []), ...RANGES]
  return (
    <div role="radiogroup" aria-label="Rango de tiempo" className="inline-flex rounded-md border border-zinc-800 p-0.5 text-xs">
      {opts.map((o) => (
        <button
          key={o.key}
          role="radio"
          aria-checked={value === o.key}
          onClick={() => onChange(o.key)}
          className={`rounded px-2.5 py-1 ${value === o.key ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'}`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
