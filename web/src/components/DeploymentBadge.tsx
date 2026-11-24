import type { DeploymentStatus } from '../lib/projects.ts'

// Icono + texto siempre; los colores de estado (verde/rojo) solo para resultados finales.
const styles: Record<DeploymentStatus, { label: string; icon: string; color: string; spin?: boolean }> = {
  queued: { label: 'En cola', icon: '○', color: 'text-zinc-400' },
  building: { label: 'Construyendo', icon: '◌', color: 'text-[#3987e5]', spin: true },
  deploying: { label: 'Publicando', icon: '◌', color: 'text-[#3987e5]', spin: true },
  succeeded: { label: 'Exitoso', icon: '●', color: 'text-[#0ca30c]' },
  failed: { label: 'Fallido', icon: '✕', color: 'text-[#d03b3b]' },
  canceled: { label: 'Cancelado', icon: '–', color: 'text-zinc-500' },
}

export function DeploymentBadge({ status }: { status: DeploymentStatus }) {
  const s = styles[status]
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-zinc-300">
      <span className={`${s.color} ${s.spin ? 'inline-block animate-spin' : ''}`} aria-hidden>
        {s.icon}
      </span>
      {s.label}
    </span>
  )
}
