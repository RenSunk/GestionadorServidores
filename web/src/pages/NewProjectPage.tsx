import { type FormEvent, type ReactNode, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { useCreateProject } from '../lib/projects.ts'
import { useServers } from '../lib/servers.ts'

const inputClass =
  'mt-1 w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none'

export function NewProjectPage() {
  const navigate = useNavigate()
  const servers = useServers()
  const create = useCreateProject()
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [form, setForm] = useState({
    name: '',
    serverId: '',
    repoUrl: '',
    branch: 'main',
    gitUsername: '',
    gitToken: '',
    rootDir: '',
    dockerfile: 'Dockerfile',
    containerPort: '3000',
    hostPort: '',
  })

  const set = (field: keyof typeof form) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))

  const serverId = form.serverId || servers.data?.[0]?.id || ''

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    create.mutate(
      {
        name: form.name,
        serverId,
        repoUrl: form.repoUrl,
        branch: form.branch,
        gitUsername: form.gitUsername || undefined,
        gitToken: form.gitToken || undefined,
        rootDir: form.rootDir || undefined,
        dockerfile: form.dockerfile || undefined,
        containerPort: Number(form.containerPort),
        hostPort: form.hostPort ? Number(form.hostPort) : null,
      },
      { onSuccess: (p) => navigate(`/projects/${p.id}`) },
    )
  }

  if (servers.data?.length === 0) {
    return (
      <div className="max-w-2xl">
        <h1 className="text-2xl font-semibold">Nuevo proyecto</h1>
        <p className="mt-4 text-sm text-zinc-400">
          Primero necesitas un servidor.{' '}
          <Link to="/servers/new" className="text-zinc-200 underline">
            Agregar servidor
          </Link>
        </p>
      </div>
    )
  }

  return (
    <div className="max-w-2xl">
      <Link to="/projects" className="text-sm text-zinc-500 hover:text-zinc-300">
        ← Proyectos
      </Link>
      <h1 className="mt-2 text-2xl font-semibold">Nuevo proyecto</h1>
      <p className="mt-1 text-sm text-zinc-400">
        El repositorio debe tener un Dockerfile. Se verifica el acceso al repo antes de guardar.
      </p>

      <form onSubmit={onSubmit} className="mt-8 space-y-6">
        <Field label="Nombre">
          <input required value={form.name} onChange={set('name')} placeholder="mi-api" className={inputClass} />
        </Field>

        <Field label="Servidor">
          <select required value={serverId} onChange={set('serverId')} className={inputClass}>
            {servers.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.host}){s.system.dockerStatus !== 'running' ? ' — Docker no disponible' : ''}
              </option>
            ))}
          </select>
        </Field>

        <div className="grid grid-cols-[1fr_9rem] gap-3">
          <Field label="URL del repositorio (HTTPS)">
            <input
              required
              type="url"
              value={form.repoUrl}
              onChange={set('repoUrl')}
              placeholder="https://github.com/usuario/repo.git"
              className={inputClass}
            />
          </Field>
          <Field label="Rama">
            <input required value={form.branch} onChange={set('branch')} className={inputClass} />
          </Field>
        </div>

        <Field label="Token de acceso" hint="Solo para repos privados. Permiso de lectura del contenido. Se guarda cifrado.">
          <input
            type="password"
            value={form.gitToken}
            onChange={set('gitToken')}
            autoComplete="off"
            placeholder="ghp_… / glpat-…"
            className={inputClass}
          />
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Puerto del contenedor" hint="El puerto donde escucha tu app dentro del contenedor.">
            <input required type="number" min={1} max={65535} value={form.containerPort} onChange={set('containerPort')} className={inputClass} />
          </Field>
          <Field label="Puerto publicado en el servidor" hint="Opcional. Vacío = no se expone (los dominios llegan en la fase 4).">
            <input type="number" min={1} max={65535} value={form.hostPort} onChange={set('hostPort')} placeholder="8080" className={inputClass} />
          </Field>
        </div>

        <div>
          <button type="button" onClick={() => setShowAdvanced((v) => !v)} className="text-sm text-zinc-400 hover:text-zinc-200">
            {showAdvanced ? '▾' : '▸'} Opciones avanzadas
          </button>
          {showAdvanced && (
            <div className="mt-4 space-y-6 border-l border-zinc-800 pl-4">
              <div className="grid grid-cols-2 gap-3">
                <Field label="Carpeta raíz" hint="Para monorepos. Vacío = raíz del repo.">
                  <input value={form.rootDir} onChange={set('rootDir')} placeholder="apps/api" className={inputClass} />
                </Field>
                <Field label="Dockerfile" hint="Relativo a la carpeta raíz.">
                  <input value={form.dockerfile} onChange={set('dockerfile')} className={inputClass} />
                </Field>
              </div>
              <Field label="Usuario de Git" hint="Solo lo exige Bitbucket. GitHub, GitLab y Gitea funcionan con el token solo.">
                <input value={form.gitUsername} onChange={set('gitUsername')} className={inputClass} />
              </Field>
            </div>
          )}
        </div>

        {create.isError && (
          <div className="rounded-md border border-[#d03b3b]/50 bg-[#d03b3b]/10 p-3 text-sm text-zinc-200">
            <span className="mr-1.5 text-[#d03b3b]">✕</span>
            {create.error.message}
          </div>
        )}

        <div className="flex items-center gap-3">
          <button
            type="submit"
            disabled={create.isPending}
            className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-60"
          >
            {create.isPending ? 'Verificando repositorio…' : 'Crear proyecto'}
          </button>
          <Link to="/projects" className="text-sm text-zinc-400 hover:text-zinc-200">
            Cancelar
          </Link>
        </div>
      </form>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="text-sm text-zinc-300">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-zinc-500">{hint}</span>}
    </label>
  )
}
