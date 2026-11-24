import { Link } from 'react-router'
import { useAccess } from '../lib/teams.ts'

/** Quién ve el recurso (solo se muestra a administradores). */
export function AccessBadge({ kind, id }: { kind: 'project' | 'server'; id: string }) {
  const teams = useAccess(kind, id)
  if (!teams) return null
  return (
    <Link
      to="/settings"
      title="Se cambia en Configuración → Equipos"
      className="inline-block rounded border border-zinc-700 px-1.5 py-0.5 text-[11px] text-zinc-400 hover:text-zinc-200"
    >
      {teams.length === 0 ? 'Visible para todos' : `Solo ${teams.length === 1 ? 'equipo' : 'equipos'} ${teams.join(', ')}`}
    </Link>
  )
}
