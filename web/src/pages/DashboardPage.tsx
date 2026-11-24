import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { api, type Health } from '../lib/api.ts'
import { useServers } from '../lib/servers.ts'

export function DashboardPage() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: () => api<Health>('/health'),
    refetchInterval: 10_000,
  })
  const servers = useServers()

  const total = servers.data?.length ?? 0
  const online = servers.data?.filter((s) => s.status === 'online').length ?? 0
  const offline = servers.data?.filter((s) => s.status === 'offline') ?? []

  return (
    <div className="max-w-4xl">
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      <p className="mt-1 text-sm text-zinc-400">Resumen de la instancia.</p>

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Stat label="Servidores en línea" value={servers.data ? `${online} de ${total}` : '…'} />
        <Stat
          label="API"
          value={health.isPending ? '…' : health.isError ? 'Sin conexión' : health.data.status === 'ok' ? 'Operativa' : 'Degradada'}
        />
        <Stat label="Base de datos" value={health.data ? `${health.data.database} · ${health.data.version}` : '—'} />
      </div>

      {offline.length > 0 && (
        <div className="mt-6 rounded-lg border border-[#d03b3b]/50 bg-[#d03b3b]/10 p-4 text-sm">
          <div className="font-medium">
            <span className="mr-1.5 text-[#d03b3b]">✕</span>
            {offline.length === 1 ? '1 servidor sin conexión' : `${offline.length} servidores sin conexión`}
          </div>
          <ul className="mt-2 space-y-1">
            {offline.map((s) => (
              <li key={s.id}>
                <Link to={`/servers/${s.id}`} className="text-zinc-200 underline hover:text-white">
                  {s.name}
                </Link>
                <span className="ml-2 text-zinc-500">{s.lastError}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {servers.data?.length === 0 && (
        <div className="mt-6 rounded-lg border border-dashed border-zinc-800 p-6 text-sm">
          <p className="font-medium">Empieza agregando un servidor</p>
          <p className="mt-1 text-zinc-400">Necesitas acceso SSH a una máquina Linux.</p>
          <Link to="/servers/new" className="mt-3 inline-block text-zinc-200 underline hover:text-white">
            Agregar servidor
          </Link>
        </div>
      )}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <div className="text-xs uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="mt-1 text-lg font-medium">{value}</div>
    </div>
  )
}
