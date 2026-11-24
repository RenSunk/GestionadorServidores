import { formatPercent } from '../lib/format.ts'

type Props = {
  label: string
  percent: number
  detail?: string
  compact?: boolean
}

const HIGH_USAGE = 90

// Barra de uso de un solo tono (azul sobre su propia rampa). Por encima del 90%
// se agrega un aviso con icono + texto en lugar de cambiar el color de la barra.
export function Meter({ label, percent, detail, compact }: Props) {
  const value = Math.max(0, Math.min(100, percent))
  const high = value >= HIGH_USAGE

  return (
    <div>
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="text-zinc-400">{label}</span>
        <span className="tabular-nums text-zinc-100">
          {high && (
            <span className="mr-1.5 text-[#fab219]" title="Uso alto">
              ▲ alto
            </span>
          )}
          {formatPercent(value)}
        </span>
      </div>
      <div
        className={`mt-1.5 overflow-hidden rounded-full bg-[#0d366b]/60 ${compact ? 'h-1.5' : 'h-2'}`}
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(value)}
      >
        <div className="h-full rounded-full bg-[#3987e5] transition-[width] duration-500" style={{ width: `${value}%` }} />
      </div>
      {detail && !compact && <div className="mt-1 text-xs tabular-nums text-zinc-500">{detail}</div>}
    </div>
  )
}
