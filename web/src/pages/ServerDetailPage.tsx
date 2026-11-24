import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { CopyButton } from '../components/CopyButton.tsx'
import { Meter } from '../components/Meter.tsx'
import { StatusBadge } from '../components/StatusBadge.tsx'
import { UsageChart } from '../components/UsageChart.tsx'
import { formatBytes, formatRelative, formatUptime } from '../lib/format.ts'
import { type ServerDetail, useCheckServer, useDeleteServer, useServer, usagePercent } from '../lib/servers.ts'

export function ServerDetailPage() {
  const { id = '' } = useParams()
  const { data: s, isPending, isError, error } = useServer(id)

  if (isPending) return <p className="text-sm text-zinc-500">Cargando…</p>
  if (isError) {
    return (
      <div>
        <p className="text-sm text-red-400">{error.message}</p>
        <Link to="/servers" className="mt-2 inline-block text-sm text-zinc-400 underline">
          Volver a servidores
        </Link>
      </div>
    )
  }

  const m = s.metrics

  return (
    <div className="max-w-5xl">
      <Link to="/servers" className="text-sm text-zinc-500 hover:text-zinc-300">
        ← Servidores
      </Link>

      <Header server={s} />

      {s.status === 'offline' && s.lastError && (
        <div className="mt-6 rounded-md border border-[#d03b3b]/50 bg-[#d03b3b]/10 p-3 text-sm text-zinc-200">
          <span className="mr-1.5 text-[#d03b3b]">✕</span>
          {s.lastError}
          <div className="mt-1 text-xs text-zinc-500">Última conexión exitosa: {formatRelative(s.lastSeenAt)}</div>
        </div>
      )}

      {s.status === 'online' && <DockerNotice server={s} />}

      {m && (
        <section className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Card>
            <Meter label="CPU" percent={m.cpuPercent} detail={`${s.system.cpuCores} núcleos · carga ${m.load.map((l) => l.toFixed(2)).join(' ')}`} />
          </Card>
          <Card>
            <Meter label="RAM" percent={usagePercent(m.memory)} detail={`${formatBytes(m.memory.used)} de ${formatBytes(m.memory.total)}`} />
          </Card>
          <Card>
            <Meter label="Disco (/)" percent={usagePercent(m.disk)} detail={`${formatBytes(m.disk.used)} de ${formatBytes(m.disk.total)}`} />
          </Card>
          <Card>
            {m.swap.total > 0 ? (
              <Meter label="Swap" percent={usagePercent(m.swap)} detail={`${formatBytes(m.swap.used)} de ${formatBytes(m.swap.total)}`} />
            ) : (
              <div className="text-xs">
                <div className="text-zinc-400">Swap</div>
                <div className="mt-1.5 text-zinc-500">No configurado</div>
              </div>
            )}
          </Card>
        </section>
      )}

      <section className="mt-4 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
        <h2 className="text-sm font-medium">Uso en los últimos 30 minutos</h2>
        <div className="mt-3">
          <UsageChart points={s.history} />
        </div>
      </section>

      <section className="mt-4 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
        <h2 className="text-sm font-medium">Sistema</h2>
        <dl className="mt-3 grid grid-cols-1 gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
          <Info label="Sistema operativo" value={s.system.os} />
          <Info label="Kernel" value={s.system.kernel} />
          <Info label="Arquitectura" value={s.system.arch} />
          <Info label="Hostname" value={s.system.hostname} />
          <Info label="Docker" value={s.system.dockerVersion || dockerText[s.system.dockerStatus]} />
          <Info label="Encendido hace" value={m ? formatUptime(m.uptimeSeconds) : '—'} />
          <Info label="Autenticación" value={authText[s.authMethod]} />
          <Info label="Llave del host" value={<span className="font-mono text-xs">{s.hostKeyFingerprint || '—'}</span>} />
        </dl>
      </section>
    </div>
  )
}

const dockerText: Record<ServerDetail['system']['dockerStatus'], string> = {
  '': '—',
  running: 'Disponible',
  not_installed: 'No instalado',
  no_permission: 'Sin permisos',
  not_running: 'Detenido',
}

const authText: Record<ServerDetail['authMethod'], string> = {
  instance_key: 'Llave de Gestionador',
  private_key: 'Llave privada propia',
  password: 'Contraseña',
}

function Header({ server: s }: { server: ServerDetail }) {
  const navigate = useNavigate()
  const check = useCheckServer(s.id)
  const del = useDeleteServer()

  function onDelete() {
    if (!confirm(`¿Eliminar el servidor "${s.name}"? Gestionador dejará de monitorearlo. No se borra nada en el servidor.`)) return
    del.mutate(s.id, { onSuccess: () => navigate('/servers') })
  }

  return (
    <div className="mt-2 flex flex-wrap items-start justify-between gap-4">
      <div>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">{s.name}</h1>
          <StatusBadge status={s.status} />
        </div>
        <p className="mt-1 font-mono text-sm text-zinc-400">
          {s.username}@{s.host}
          {s.port !== 22 && `:${s.port}`}
        </p>
      </div>
      <div className="flex gap-2">
        <button
          onClick={() => check.mutate()}
          disabled={check.isPending}
          className="rounded-md border border-zinc-700 px-3 py-1.5 text-sm text-zinc-200 hover:bg-zinc-800 disabled:opacity-60"
        >
          {check.isPending ? 'Verificando…' : 'Verificar ahora'}
        </button>
        <button
          onClick={onDelete}
          disabled={del.isPending}
          className="rounded-md border border-zinc-700 px-3 py-1.5 text-sm text-zinc-400 hover:border-[#d03b3b]/60 hover:text-zinc-100 disabled:opacity-60"
        >
          Eliminar
        </button>
      </div>
      {del.isError && <p className="w-full text-sm text-red-400">{del.error.message}</p>}
    </div>
  )
}

// Explica cómo dejar Docker listo cuando no está disponible.
function DockerNotice({ server: s }: { server: ServerDetail }) {
  const status = s.system.dockerStatus
  if (status === 'running' || status === '') return null

  const content = {
    not_installed: {
      title: 'Docker no está instalado',
      text: 'Instálalo con el script oficial (requiere root o sudo):',
      command: 'curl -fsSL https://get.docker.com | sudo sh',
    },
    no_permission: {
      title: `El usuario ${s.username} no tiene permisos sobre Docker`,
      text: 'Agrégalo al grupo docker y luego pulsa "Verificar ahora":',
      command: `sudo usermod -aG docker ${s.username}`,
    },
    not_running: {
      title: 'Docker está instalado pero detenido',
      text: 'Inícialo y habilítalo al arranque:',
      command: 'sudo systemctl enable --now docker',
    },
  }[status]

  return (
    <div className="mt-6 rounded-md border border-[#fab219]/40 bg-[#fab219]/5 p-4 text-sm">
      <div className="font-medium text-zinc-100">
        <span className="mr-1.5 text-[#fab219]">▲</span>
        {content.title}
      </div>
      <p className="mt-1 text-zinc-400">{content.text}</p>
      <div className="mt-2 flex items-center gap-2">
        <code className="min-w-0 flex-1 overflow-x-auto rounded bg-zinc-950 p-2 font-mono text-xs text-zinc-300">{content.command}</code>
        <CopyButton text={content.command} />
      </div>
      <p className="mt-2 text-xs text-zinc-500">Docker es necesario para desplegar proyectos en este servidor.</p>
    </div>
  )
}

function Card({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">{children}</div>
}

function Info({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex justify-between gap-4 border-b border-zinc-800/60 py-1.5">
      <dt className="text-zinc-500">{label}</dt>
      <dd className="truncate text-right text-zinc-200">{value || '—'}</dd>
    </div>
  )
}
