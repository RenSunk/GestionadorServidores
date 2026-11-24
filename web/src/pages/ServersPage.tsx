import { useCan } from '../lib/auth.ts'
import { Link } from 'react-router'
import { Meter } from '../components/Meter.tsx'
import { StatusBadge } from '../components/StatusBadge.tsx'
import { formatRelative } from '../lib/format.ts'
import { type Server, useServers, usagePercent } from '../lib/servers.ts'

export function ServersPage() {
  const can = useCan()
  const { data: servers, isPending, isError, error } = useServers()

  return (
    <div className="max-w-6xl">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Servidores</h1>
          <p className="mt-1 text-sm text-zinc-400">Máquinas donde se despliegan tus proyectos.</p>
        </div>
        {can('admin') && (
          <Link to="/servers/new" className="rounded-md bg-zinc-100 px-3 py-2 text-sm font-medium text-zinc-900 hover:bg-white">
            Agregar servidor
          </Link>
        )}
      </div>

      <div className="mt-6">
        {isPending && <p className="text-sm text-zinc-500">Cargando…</p>}
        {isError && <p className="text-sm text-red-400">Error: {error.message}</p>}
        {servers?.length === 0 && <EmptyState />}
        {servers && servers.length > 0 && (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {servers.map((s) => (
              <ServerCard key={s.id} server={s} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export function ServerCard({ server: s }: { server: Server }) {
  const m = s.metrics
  return (
    <Link
      to={`/servers/${s.id}`}
      className="block rounded-lg border border-zinc-800 bg-zinc-900/50 p-4 transition-colors hover:border-zinc-700 hover:bg-zinc-900"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-medium">{s.name}</div>
          <div className="truncate text-xs text-zinc-500">
            {s.username}@{s.host}
            {s.port !== 22 && `:${s.port}`}
          </div>
        </div>
        <StatusBadge status={s.status} />
      </div>

      {m ? (
        <div className="mt-4 space-y-3">
          <Meter compact label="CPU" percent={m.cpuPercent} />
          <Meter compact label="RAM" percent={usagePercent(m.memory)} />
          <Meter compact label="Disco" percent={usagePercent(m.disk)} />
        </div>
      ) : (
        <p className="mt-4 line-clamp-2 text-xs text-zinc-500">
          {s.status === 'offline' ? s.lastError : 'Esperando la primera muestra…'}
        </p>
      )}

      <div className="mt-4 flex items-center justify-between border-t border-zinc-800 pt-3 text-xs text-zinc-500">
        <span>{s.system.os || '—'}</span>
        <span>{s.status === 'online' ? <DockerLabel server={s} /> : `Visto ${formatRelative(s.lastSeenAt)}`}</span>
      </div>
    </Link>
  )
}

function DockerLabel({ server }: { server: Server }) {
  const { dockerStatus, dockerVersion } = server.system
  if (dockerStatus === 'running') return <>Docker {dockerVersion}</>
  return <span className="text-[#fab219]">▲ Docker no disponible</span>
}

function EmptyState() {
  const can = useCan()
  return (
    <div className="rounded-lg border border-dashed border-zinc-800 p-10 text-center">
      <p className="font-medium">Aún no hay servidores</p>
      <p className="mt-1 text-sm text-zinc-400">Agrega tu primer servidor para empezar a desplegar proyectos.</p>
      {can('admin') && (
        <Link to="/servers/new" className="mt-4 inline-block text-sm text-zinc-200 underline hover:text-white">
          Agregar servidor
        </Link>
      )}
    </div>
  )
}
