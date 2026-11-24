import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from './api.ts'

// Agregado del backend: promedio y máximo del período, en porcentaje.
export type MetricPoint = { t: string; cpu: number; cpuMax: number; mem: number; memMax: number; disk: number | null }

export type MetricRange = '1h' | '6h' | '24h' | '7d' | '30d'

export const RANGES: { key: MetricRange; label: string; ms: number }[] = [
  { key: '1h', label: '1 h', ms: 3_600_000 },
  { key: '6h', label: '6 h', ms: 6 * 3_600_000 },
  { key: '24h', label: '24 h', ms: 24 * 3_600_000 },
  { key: '7d', label: '7 días', ms: 7 * 86_400_000 },
  { key: '30d', label: '30 días', ms: 30 * 86_400_000 },
]

const MAX_POINTS = 300

/** Separación esperada entre puntos (para cortar la línea donde faltan datos). */
export function expectedStep(range: MetricRange): number {
  const r = RANGES.find((x) => x.key === range)!
  const base = r.ms > 26 * 3_600_000 ? 3_600_000 : 60_000 // tabla por hora o por minuto
  return Math.max(base, r.ms / MAX_POINTS)
}

function useMetrics(kind: 'servers' | 'projects', id: string, range: MetricRange) {
  return useQuery({
    queryKey: [kind, id, 'metrics', range],
    queryFn: () => api<MetricPoint[]>(`/${kind}/${id}/metrics?range=${range}`),
    refetchInterval: range === '1h' ? 60_000 : 5 * 60_000,
    placeholderData: keepPreviousData,
  })
}

export const useServerMetrics = (id: string, range: MetricRange) => useMetrics('servers', id, range)
export const useProjectMetrics = (id: string, range: MetricRange) => useMetrics('projects', id, range)
