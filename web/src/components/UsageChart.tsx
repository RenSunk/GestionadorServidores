import { useEffect, useRef, useState } from 'react'
import { formatPercent, formatTime } from '../lib/format.ts'
import type { HistoryPoint } from '../lib/servers.ts'

// Dos series en la misma escala (0–100 %), así que un solo eje.
// Colores: slots 1 y 2 de la paleta categórica validada, variante oscura.
const SERIES = [
  { key: 'cpuPercent', label: 'CPU', color: '#3987e5' },
  { key: 'memoryPercent', label: 'RAM', color: '#d95926' },
] as const

const HEIGHT = 200
const PAD = { top: 12, right: 72, bottom: 24, left: 36 }

export function UsageChart({ points }: { points: HistoryPoint[] }) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    if (!ref.current) return
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    ro.observe(ref.current)
    return () => ro.disconnect()
  }, [])

  if (points.length < 2) {
    return (
      <div className="flex h-[200px] items-center justify-center text-sm text-zinc-500">
        Recolectando datos… la gráfica aparece tras la segunda muestra (cada 15 s).
      </div>
    )
  }

  const innerW = Math.max(1, width - PAD.left - PAD.right)
  const innerH = HEIGHT - PAD.top - PAD.bottom
  const x = (i: number) => PAD.left + (i / (points.length - 1)) * innerW
  const y = (v: number) => PAD.top + innerH - (Math.min(100, Math.max(0, v)) / 100) * innerH

  const paths = SERIES.map((s) => points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i)},${y(p[s.key])}`).join(''))
  const last = points[points.length - 1]

  function onMove(e: React.PointerEvent<SVGRectElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const ratio = (e.clientX - rect.left) / rect.width
    setHover(Math.round(ratio * (points.length - 1)))
  }

  const hp = hover !== null ? points[hover] : null

  return (
    <div ref={ref} className="relative">
      <Legend />
      <svg width={width} height={HEIGHT} className="mt-2 block" role="img" aria-label="Uso de CPU y RAM en el tiempo">
        {/* Rejilla recesiva */}
        {[0, 50, 100].map((v) => (
          <g key={v}>
            <line x1={PAD.left} x2={PAD.left + innerW} y1={y(v)} y2={y(v)} stroke="#3f3f46" strokeWidth={1} strokeDasharray={v === 0 ? undefined : '2 4'} />
            <text x={PAD.left - 8} y={y(v)} dy="0.32em" textAnchor="end" className="fill-zinc-500 text-[10px]">
              {v}%
            </text>
          </g>
        ))}
        <text x={PAD.left} y={HEIGHT - 6} className="fill-zinc-500 text-[10px]">
          {formatTime(points[0].t)}
        </text>
        <text x={PAD.left + innerW} y={HEIGHT - 6} textAnchor="end" className="fill-zinc-500 text-[10px]">
          {formatTime(last.t)}
        </text>

        {SERIES.map((s, i) => (
          <path key={s.key} d={paths[i]} fill="none" stroke={s.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        ))}

        {/* Etiquetas directas con el último valor */}
        {SERIES.map((s) => (
          <text key={s.key} x={PAD.left + innerW + 6} y={y(last[s.key])} dy="0.32em" className="fill-zinc-300 text-[10px] tabular-nums">
            {s.label} {formatPercent(last[s.key])}
          </text>
        ))}

        {hp && hover !== null && (
          <g pointerEvents="none">
            <line x1={x(hover)} x2={x(hover)} y1={PAD.top} y2={PAD.top + innerH} stroke="#71717a" strokeWidth={1} />
            {SERIES.map((s) => (
              <circle key={s.key} cx={x(hover)} cy={y(hp[s.key])} r={4} fill={s.color} stroke="#18181b" strokeWidth={2} />
            ))}
          </g>
        )}

        {/* Área de captura más grande que las líneas */}
        <rect
          x={PAD.left}
          y={PAD.top}
          width={innerW}
          height={innerH}
          fill="transparent"
          onPointerMove={onMove}
          onPointerLeave={() => setHover(null)}
        />
      </svg>

      {hp && hover !== null && (
        <div
          className="pointer-events-none absolute top-8 rounded-md border border-zinc-700 bg-zinc-900 px-2.5 py-1.5 text-xs shadow-lg"
          style={{ left: Math.min(x(hover) + 12, width - 140) }}
        >
          <div className="text-zinc-400">{formatTime(hp.t)}</div>
          {SERIES.map((s) => (
            <div key={s.key} className="flex items-center gap-2 tabular-nums text-zinc-100">
              <span className="inline-block h-0.5 w-3 rounded" style={{ background: s.color }} />
              {s.label} {formatPercent(hp[s.key])}
            </div>
          ))}
        </div>
      )}

      <details className="mt-2 text-xs text-zinc-400">
        <summary className="cursor-pointer hover:text-zinc-200">Ver como tabla</summary>
        <table className="mt-2 w-full max-w-sm tabular-nums">
          <thead className="text-left text-zinc-500">
            <tr>
              <th className="py-1 font-normal">Hora</th>
              <th className="py-1 font-normal">CPU</th>
              <th className="py-1 font-normal">RAM</th>
            </tr>
          </thead>
          <tbody className="text-zinc-300">
            {points
              .slice(-10)
              .reverse()
              .map((p) => (
                <tr key={p.t}>
                  <td className="py-0.5">{formatTime(p.t)}</td>
                  <td className="py-0.5">{formatPercent(p.cpuPercent)}</td>
                  <td className="py-0.5">{formatPercent(p.memoryPercent)}</td>
                </tr>
              ))}
          </tbody>
        </table>
      </details>
    </div>
  )
}

function Legend() {
  return (
    <div className="flex gap-4 text-xs text-zinc-400">
      {SERIES.map((s) => (
        <span key={s.key} className="inline-flex items-center gap-1.5">
          <span className="inline-block h-0.5 w-4 rounded" style={{ background: s.color }} />
          {s.label}
        </span>
      ))}
    </div>
  )
}
