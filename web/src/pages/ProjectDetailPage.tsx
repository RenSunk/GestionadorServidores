import { useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { DeploymentBadge } from '../components/DeploymentBadge.tsx'
import { LogViewer } from '../components/LogViewer.tsx'
import { Meter } from '../components/Meter.tsx'
import { formatDuration, formatRelative } from '../lib/format.ts'
import {
  type ContainerStatus,
  type ProjectDetail,
  useContainerStatus,
  useDeleteProject,
  useDeploy,
  repoName,
  useProject,
} from '../lib/projects.ts'

type Tab = 'deployments' | 'logs' | 'settings'

export function ProjectDetailPage() {
  const { id = '' } = useParams()
  const { data: p, isPending, isError, error } = useProject(id)
  const [tab, setTab] = useState<Tab>('deployments')

  if (isPending) return <p className="text-sm text-zinc-500">Cargando…</p>
  if (isError) {
    return (
      <div>
        <p className="text-sm text-red-400">{error.message}</p>
        <Link to="/projects" className="mt-2 inline-block text-sm text-zinc-400 underline">
          Volver a proyectos
        </Link>
      </div>
    )
  }

  return (
    <div className="max-w-5xl">
      <Link to="/projects" className="text-sm text-zinc-500 hover:text-zinc-300">
        ← Proyectos
      </Link>
      <Header project={p} />
      <ContainerCard project={p} />

      <div className="mt-6 flex gap-1 border-b border-zinc-800">
        {(
          [
            ['deployments', 'Deploys'],
            ['logs', 'Logs'],
            ['settings', 'Configuración'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            onClick={() => setTab(key)}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${
              tab === key ? 'border-zinc-100 text-zinc-100' : 'border-transparent text-zinc-500 hover:text-zinc-300'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="mt-4">
        {tab === 'deployments' && <DeploymentsList project={p} />}
        {tab === 'logs' && (
          <LogViewer url={`/api/projects/${p.id}/logs?tail=300`} emptyText="Esperando logs del contenedor…" />
        )}
        {tab === 'settings' && <Settings project={p} />}
      </div>
    </div>
  )
}

function Header({ project: p }: { project: ProjectDetail }) {
  const navigate = useNavigate()
  const deploy = useDeploy(p.id)
  const active = p.activeDeploymentId

  return (
    <div className="mt-2 flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold">{p.name}</h1>
        <p className="mt-1 text-sm text-zinc-400">
          <a href={p.repoUrl} target="_blank" rel="noreferrer" className="hover:text-zinc-200 hover:underline">
            {repoName(p.repoUrl)}
          </a>{' '}
          · {p.branch} · en{' '}
          <Link to={`/servers/${p.serverId}`} className="hover:text-zinc-200 hover:underline">
            {p.serverName}
          </Link>
        </p>
        {p.url && (
          <a href={p.url} target="_blank" rel="noreferrer" className="mt-1 inline-block text-sm text-zinc-200 underline hover:text-white">
            {p.url} ↗
          </a>
        )}
      </div>
      <div className="flex flex-col items-end gap-2">
        {active ? (
          <Link
            to={`/projects/${p.id}/deployments/${active}`}
            className="rounded-md border border-zinc-700 px-4 py-2 text-sm text-zinc-200 hover:bg-zinc-800"
          >
            <span className="mr-1.5 inline-block animate-spin text-[#3987e5]">◌</span>
            Ver deploy en curso
          </Link>
        ) : (
          <button
            onClick={() => deploy.mutate(undefined, { onSuccess: (d) => navigate(`/projects/${p.id}/deployments/${d.id}`) })}
            disabled={deploy.isPending}
            className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-60"
          >
            {deploy.isPending ? 'Iniciando…' : 'Desplegar'}
          </button>
        )}
        {deploy.isError && <p className="text-xs text-red-400">{deploy.error.message}</p>}
      </div>
    </div>
  )
}

const stateText: Record<ContainerStatus['state'], string> = {
  running: 'En ejecución',
  exited: 'Detenido',
  restarting: 'Reiniciándose',
  created: 'Creado',
  paused: 'Pausado',
  dead: 'Muerto',
  missing: 'Sin contenedor',
  unknown: 'Desconocido',
}

function ContainerCard({ project: p }: { project: ProjectDetail }) {
  const { data: st } = useContainerStatus(p.id)

  const icon =
    st?.state === 'running' ? (
      <span className="text-[#0ca30c]">●</span>
    ) : st?.state === 'missing' || st?.state === 'unknown' ? (
      <span className="text-zinc-500">○</span>
    ) : (
      <span className="text-[#d03b3b]">✕</span>
    )

  return (
    <section className="mt-6 grid grid-cols-1 gap-4 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4 sm:grid-cols-4">
      <div>
        <div className="text-xs text-zinc-400">Contenedor</div>
        <div className="mt-1.5 flex items-center gap-1.5 text-sm text-zinc-100">
          {st ? (
            <>
              {icon} {stateText[st.state]}
            </>
          ) : (
            '…'
          )}
        </div>
        <div className="mt-1 text-xs text-zinc-500">
          {st?.state === 'running' && st.startedAt ? `Desde ${formatRelative(st.startedAt)}` : null}
          {st?.state === 'missing' && 'Aún no se ha desplegado'}
          {st?.state === 'unknown' && st.error}
          {!!st?.restartCount && ` · ${st.restartCount} reinicios`}
        </div>
      </div>
      {st?.state === 'running' ? (
        <>
          <Meter label="CPU" percent={st.cpuPercent ?? 0} detail="100% = un núcleo completo" />
          <Meter label="Memoria" percent={st.memoryPercent ?? 0} detail={st.memoryUsage} />
          <div className="text-xs">
            <div className="text-zinc-400">Red (entrada / salida)</div>
            <div className="mt-1.5 tabular-nums text-zinc-100">{st.netIO || '—'}</div>
          </div>
        </>
      ) : (
        <div className="text-xs text-zinc-500 sm:col-span-3 sm:self-center">El consumo se muestra cuando el contenedor está en ejecución.</div>
      )}
    </section>
  )
}

function DeploymentsList({ project: p }: { project: ProjectDetail }) {
  if (p.deployments.length === 0) {
    return <p className="text-sm text-zinc-500">Todavía no hay deploys. Pulsa “Desplegar” para el primero.</p>
  }
  return (
    <ul className="divide-y divide-zinc-800 overflow-hidden rounded-lg border border-zinc-800">
      {p.deployments.map((d) => (
        <li key={d.id}>
          <Link
            to={`/projects/${p.id}/deployments/${d.id}`}
            className="grid grid-cols-[8rem_1fr_auto] items-center gap-4 px-4 py-3 text-sm hover:bg-zinc-900/50"
          >
            <DeploymentBadge status={d.status} />
            <div className="min-w-0">
              <div className="truncate text-zinc-200">{d.commitMessage || (d.error ? d.error : 'Preparando…')}</div>
              <div className="text-xs text-zinc-500">
                {d.commitSha && <span className="font-mono">{d.commitSha.slice(0, 7)} · </span>}
                {formatRelative(d.createdAt)}
              </div>
            </div>
            <div className="text-xs tabular-nums text-zinc-500">{formatDuration(d.startedAt, d.finishedAt)}</div>
          </Link>
        </li>
      ))}
    </ul>
  )
}

function Settings({ project: p }: { project: ProjectDetail }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const del = useDeleteProject()

  function onDelete() {
    if (!confirm(`¿Eliminar "${p.name}"? Se borrarán su contenedor, sus imágenes en ${p.serverName} y el historial de deploys.`)) return
    del.mutate(p.id, {
      onSuccess: ({ warning }) => {
        if (warning) alert(warning)
        qc.invalidateQueries({ queryKey: ['projects'] })
        navigate('/projects')
      },
    })
  }

  return (
    <div className="space-y-4">
      <section className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
        <dl className="grid grid-cols-1 gap-x-8 text-sm sm:grid-cols-2">
          <Info label="Repositorio" value={p.repoUrl} />
          <Info label="Rama" value={p.branch} />
          <Info label="Token de acceso" value={p.hasToken ? 'Configurado (cifrado)' : 'Ninguno (repo público)'} />
          <Info label="Carpeta raíz" value={p.rootDir || '/'} />
          <Info label="Dockerfile" value={p.dockerfile} />
          <Info label="Puerto del contenedor" value={String(p.containerPort)} />
          <Info label="Puerto publicado" value={p.hostPort ? String(p.hostPort) : 'No publicado'} />
          <Info label="Contenedor" value={<span className="font-mono text-xs">{p.containerName}</span>} />
        </dl>
        <p className="mt-3 text-xs text-zinc-500">
          Variables de entorno, límites de CPU/RAM y edición de estos datos llegan en la fase 3.
        </p>
      </section>

      <section className="rounded-lg border border-[#d03b3b]/40 p-4">
        <h2 className="text-sm font-medium">Eliminar proyecto</h2>
        <p className="mt-1 text-sm text-zinc-400">Detiene y borra el contenedor y las imágenes en el servidor. No se puede deshacer.</p>
        <button
          onClick={onDelete}
          disabled={del.isPending || !!p.activeDeploymentId}
          className="mt-3 rounded-md border border-[#d03b3b]/60 px-3 py-1.5 text-sm text-zinc-100 hover:bg-[#d03b3b]/20 disabled:opacity-50"
        >
          {del.isPending ? 'Eliminando…' : 'Eliminar proyecto'}
        </button>
        {p.activeDeploymentId && <p className="mt-2 text-xs text-zinc-500">Espera a que termine el deploy en curso.</p>}
        {del.isError && <p className="mt-2 text-sm text-red-400">{del.error.message}</p>}
      </section>
    </div>
  )
}

function Info({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex justify-between gap-4 border-b border-zinc-800/60 py-2">
      <dt className="shrink-0 text-zinc-500">{label}</dt>
      <dd className="truncate text-right text-zinc-200">{value}</dd>
    </div>
  )
}
