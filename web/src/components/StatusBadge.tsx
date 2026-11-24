import type { ServerStatus } from '../lib/servers.ts'

// Los colores de estado siempre van con icono + texto: nunca solo el color.
const styles: Record<ServerStatus, { label: string; icon: string; color: string }> = {
  online: { label: 'En línea', icon: '●', color: 'text-[#0ca30c]' },
  offline: { label: 'Sin conexión', icon: '✕', color: 'text-[#d03b3b]' },
  pending: { label: 'Pendiente', icon: '○', color: 'text-zinc-400' },
}

export function StatusBadge({ status }: { status: ServerStatus }) {
  const s = styles[status]
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-zinc-300">
      <span className={s.color} aria-hidden>
        {s.icon}
      </span>
      {s.label}
    </span>
  )
}
