import { useCan } from '../lib/auth.ts'
import { Link } from 'react-router'
import { DeploymentBadge } from '../components/DeploymentBadge.tsx'
import { formatRelative } from '../lib/format.ts'
import { type Project, repoName, useProjects } from '../lib/projects.ts'

export function ProjectsPage() {
  const can = useCan()
  const { data: projects, isPending, isError, error } = useProjects()

  return (
    <div className="max-w-6xl">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Proyectos</h1>
          <p className="mt-1 text-sm text-zinc-400">Aplicaciones desplegadas desde Git con Docker.</p>
        </div>
        {can('developer') && (
          <Link to="/projects/new" className="rounded-md bg-zinc-100 px-3 py-2 text-sm font-medium text-zinc-900 hover:bg-white">
            Nuevo proyecto
          </Link>
        )}
      </div>

      <div className="mt-6">
        {isPending && <p className="text-sm text-zinc-500">Cargando…</p>}
        {isError && <p className="text-sm text-red-400">Error: {error.message}</p>}
        {projects?.length === 0 && (
          <div className="rounded-lg border border-dashed border-zinc-800 p-10 text-center">
            <p className="font-medium">Aún no hay proyectos</p>
            <p className="mt-1 text-sm text-zinc-400">Conecta un repositorio con Dockerfile y despliégalo en uno de tus servidores.</p>
            {can('developer') && (
              <Link to="/projects/new" className="mt-4 inline-block text-sm text-zinc-200 underline hover:text-white">
                Nuevo proyecto
              </Link>
            )}
          </div>
        )}
        {projects && projects.length > 0 && (
          <div className="overflow-hidden rounded-lg border border-zinc-800">
            <table className="w-full text-sm">
              <thead className="bg-zinc-900/60 text-left text-xs text-zinc-500">
                <tr>
                  <th className="px-4 py-2 font-normal">Proyecto</th>
                  <th className="px-4 py-2 font-normal">Servidor</th>
                  <th className="px-4 py-2 font-normal">Último deploy</th>
                  <th className="px-4 py-2 font-normal">URL</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800">
                {projects.map((p) => (
                  <ProjectRow key={p.id} project={p} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}

function ProjectRow({ project: p }: { project: Project }) {
  const d = p.lastDeployment
  return (
    <tr className="hover:bg-zinc-900/40">
      <td className="px-4 py-3">
        <Link to={`/projects/${p.id}`} className="font-medium text-zinc-100 hover:underline">
          {p.name}
        </Link>
        <div className="truncate text-xs text-zinc-500">
          {repoName(p.repoUrl)} · {p.branch}
        </div>
      </td>
      <td className="px-4 py-3 text-zinc-300">{p.serverName}</td>
      <td className="px-4 py-3">
        {d ? (
          <div>
            <DeploymentBadge status={d.status} />
            <div className="text-xs text-zinc-500">
              {d.commitSha && <span className="font-mono">{d.commitSha.slice(0, 7)} · </span>}
              {formatRelative(d.createdAt)}
            </div>
          </div>
        ) : (
          <span className="text-xs text-zinc-500">Nunca desplegado</span>
        )}
      </td>
      <td className="px-4 py-3">
        {p.url ? (
          <a href={p.url} target="_blank" rel="noreferrer" className="text-xs text-zinc-300 underline hover:text-white">
            {p.url.replace(/^https?:\/\//, '')}
          </a>
        ) : (
          <span className="text-xs text-zinc-600">Sin puerto publicado</span>
        )}
      </td>
    </tr>
  )
}
