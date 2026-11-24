import { useCan } from '../lib/auth.ts'
import { useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { DeploymentBadge } from '../components/DeploymentBadge.tsx'
import { LogViewer } from '../components/LogViewer.tsx'
import { formatDuration, formatRelative } from '../lib/format.ts'
import { isActive, useCancelDeployment, useDeployment, useProject } from '../lib/projects.ts'

export function DeploymentPage() {
  const { id = '', deploymentId = '' } = useParams()
  const qc = useQueryClient()
  const project = useProject(id)
  const { data: d, isPending, isError, error } = useDeployment(deploymentId)
  const cancel = useCancelDeployment(deploymentId)
  const can = useCan()

  if (isPending) return <p className="text-sm text-zinc-500">Cargando…</p>
  if (isError) return <p className="text-sm text-red-400">{error.message}</p>

  const running = isActive(d.status)

  return (
    <div className="max-w-5xl">
      <Link to={`/projects/${id}`} className="text-sm text-zinc-500 hover:text-zinc-300">
        ← {project.data?.name ?? 'Proyecto'}
      </Link>

      <div className="mt-2 flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold">Deploy</h1>
            <DeploymentBadge status={d.status} />
          </div>
          <p className="mt-1 text-sm text-zinc-400">
            {d.commitSha ? (
              <>
                <span className="font-mono">{d.commitSha.slice(0, 7)}</span> · {d.commitMessage}
              </>
            ) : (
              'Obteniendo commit…'
            )}
          </p>
          <p className="mt-1 text-xs text-zinc-500">
            Iniciado {formatRelative(d.createdAt)} · Duración {formatDuration(d.startedAt, d.finishedAt)}
          </p>
        </div>
        {running && can('developer') && (
          <div className="flex flex-col items-end gap-1">
            <button
              onClick={() => cancel.mutate()}
              disabled={cancel.isPending || d.status === 'deploying'}
              title={d.status === 'deploying' ? 'Ya se está publicando el contenedor; no se puede cancelar' : undefined}
              className="rounded-md border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:border-[#d03b3b]/60 hover:text-zinc-100 disabled:opacity-50"
            >
              {cancel.isPending ? 'Cancelando…' : 'Cancelar deploy'}
            </button>
            {cancel.isError && <p className="max-w-xs text-right text-xs text-red-400">{cancel.error.message}</p>}
          </div>
        )}
      </div>

      {d.status === 'failed' && d.error && (
        <div className="mt-6 rounded-md border border-[#d03b3b]/50 bg-[#d03b3b]/10 p-3 text-sm text-zinc-200">
          <span className="mr-1.5 text-[#d03b3b]">✕</span>
          {d.error}
        </div>
      )}
      {d.status === 'succeeded' && project.data?.url && (
        <div className="mt-6 rounded-md border border-[#0ca30c]/40 bg-[#0ca30c]/5 p-3 text-sm text-zinc-200">
          <span className="mr-1.5 text-[#0ca30c]">●</span>
          Publicado en{' '}
          <a href={project.data.url} target="_blank" rel="noreferrer" className="underline hover:text-white">
            {project.data.url} ↗
          </a>
        </div>
      )}

      <div className="mt-6">
        <LogViewer
          url={`/api/deployments/${deploymentId}/logs`}
          height="h-[560px]"
          onEnd={() => {
            qc.invalidateQueries({ queryKey: ['deployments', deploymentId] })
            qc.invalidateQueries({ queryKey: ['projects'] })
          }}
        />
      </div>
    </div>
  )
}
