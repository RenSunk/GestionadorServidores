import { type FormEvent, type ReactNode, useState } from 'react'
import { formatBytes } from '../lib/format.ts'
import { type ProjectDetail, useRuntimes, useUpdateProject } from '../lib/projects.ts'
import { useServer } from '../lib/servers.ts'
import { inputClass } from '../lib/ui.ts'
import { type Build, BuildConfig } from './BuildConfig.tsx'
import { ErrorBox, Field } from './Form.tsx'

/** Edición de la configuración del proyecto. Los cambios se aplican en el próximo deploy. */
export function ProjectSettingsForm({ project, buildArgs }: { project: ProjectDetail; buildArgs: string[] }) {
  // key: cuando el proyecto cambia en el servidor (tras guardar), el formulario se reinicia.
  const key = JSON.stringify([
    project.name, project.repoUrl, project.branch, project.hasToken, project.gitUsername, project.rootDir, project.dockerfile,
    project.containerPort, project.hostPort, project.buildType, project.runtime, project.runtimeVersion, project.baseImage,
    project.buildCommand, project.startCommand, project.cpuLimit, project.memoryLimitMb,
  ])
  // La mutación vive fuera del formulario para que el "Guardado" sobreviva al reinicio.
  const update = useUpdateProject(project.id)
  return <Form key={key} project={project} buildArgs={buildArgs} update={update} />
}

const savedBuild = (p: ProjectDetail): Build => ({
  type: p.buildType,
  runtime: p.runtime,
  version: p.runtimeVersion,
  baseImage: p.baseImage,
  buildCommand: p.buildCommand,
  startCommand: p.startCommand,
})

function Form({ project: p, buildArgs, update }: { project: ProjectDetail; buildArgs: string[]; update: ReturnType<typeof useUpdateProject> }) {
  const runtimes = useRuntimes()
  const server = useServer(p.serverId)

  const initial = {
    name: p.name,
    repoUrl: p.repoUrl,
    branch: p.branch,
    gitToken: '',
    clearToken: false,
    gitUsername: p.gitUsername,
    rootDir: p.rootDir,
    dockerfile: p.dockerfile,
    containerPort: String(p.containerPort),
    hostPort: p.hostPort ? String(p.hostPort) : '',
    cpuLimit: p.cpuLimit != null ? String(p.cpuLimit) : '',
    memoryLimitMb: p.memoryLimitMb != null ? String(p.memoryLimitMb) : '',
  }
  const [f, setF] = useState(initial)
  const [build, setBuild] = useState<Build>(() => savedBuild(p))
  const set = (field: keyof typeof initial) => (e: { target: { value: string } }) => setF((x) => ({ ...x, [field]: e.target.value }))

  const dirty = JSON.stringify(f) !== JSON.stringify(initial) || JSON.stringify(build) !== JSON.stringify(savedBuild(p))
  const runtime = runtimes.data?.find((r) => r.id === p.runtime)

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    update.mutate({
      name: f.name,
      repoUrl: f.repoUrl,
      branch: f.branch,
      gitToken: f.gitToken || undefined,
      clearToken: f.clearToken,
      gitUsername: f.gitUsername,
      rootDir: f.rootDir,
      dockerfile: f.dockerfile,
      containerPort: Number(f.containerPort),
      hostPort: f.hostPort ? Number(f.hostPort) : null,
      cpuLimit: f.cpuLimit ? Number(f.cpuLimit) : null,
      memoryLimitMb: f.memoryLimitMb ? Number(f.memoryLimitMb) : null,
      buildType: build.type,
      ...(build.type === 'runtime' && {
        runtime: build.runtime,
        runtimeVersion: build.version,
        baseImage: build.baseImage,
        buildCommand: build.buildCommand,
        startCommand: build.startCommand,
      }),
    })
  }

  const cores = server.data?.system.cpuCores
  const memTotal = server.data?.metrics?.memory.total

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <Card title="General">
        <Field label="Nombre" hint={<>El contenedor sigue llamándose <code className="font-mono">{p.containerName}</code>.</>}>
          <input required value={f.name} onChange={set('name')} className={inputClass} />
        </Field>
        <div className="grid grid-cols-[1fr_9rem] gap-3">
          <Field label="URL del repositorio (HTTPS)">
            <input required type="url" value={f.repoUrl} onChange={set('repoUrl')} className={inputClass} />
          </Field>
          <Field label="Rama">
            <input required value={f.branch} onChange={set('branch')} className={inputClass} />
          </Field>
        </div>
        <Field label="Token de acceso" hint={p.hasToken ? 'Hay un token guardado. Déjalo vacío para conservarlo.' : 'Solo para repos privados.'}>
          <input
            type="password"
            value={f.gitToken}
            onChange={(e) => setF((x) => ({ ...x, gitToken: e.target.value, clearToken: false }))}
            disabled={f.clearToken}
            autoComplete="off"
            placeholder={p.hasToken ? '•••••••• (guardado)' : 'ghp_… / glpat-…'}
            className={inputClass}
          />
        </Field>
        {p.hasToken && (
          <label className="flex items-center gap-2 text-sm text-zinc-400">
            <input
              type="checkbox"
              checked={f.clearToken}
              onChange={(e) => setF((x) => ({ ...x, clearToken: e.target.checked, gitToken: '' }))}
              className="h-4 w-4 accent-zinc-200"
            />
            Quitar el token (el repositorio es público)
          </label>
        )}
        <div className="grid grid-cols-3 gap-3">
          <Field label="Carpeta raíz">
            <input value={f.rootDir} onChange={set('rootDir')} placeholder="/" className={inputClass} />
          </Field>
          <Field label="Ruta del Dockerfile">
            <input value={f.dockerfile} onChange={set('dockerfile')} disabled={build.type === 'runtime'} className={inputClass} />
          </Field>
          <Field label="Usuario de Git">
            <input value={f.gitUsername} onChange={set('gitUsername')} placeholder="solo Bitbucket" className={inputClass} />
          </Field>
        </div>
      </Card>

      <Card title="Build">
        {runtimes.data ? (
          <BuildConfig
            build={build}
            setBuild={setBuild}
            runtimes={runtimes.data}
            port={Number(f.containerPort) || 0}
            dockerfilePath={f.dockerfile || 'Dockerfile'}
            buildArgs={buildArgs}
            suggestion={
              p.buildType === 'runtime'
                ? { runtime: p.runtime, version: p.runtimeVersion, baseImage: p.baseImage, buildCommand: p.buildCommand, startCommand: p.startCommand, label: 'actual' }
                : undefined
            }
          />
        ) : (
          <p className="text-sm text-zinc-500">Cargando…</p>
        )}
        {runtime && build.type === 'dockerfile' && p.buildType === 'runtime' && (
          <p className="text-xs text-zinc-500">Al guardar, el próximo deploy usará el Dockerfile del repositorio en lugar del generado ({p.runtimeLabel}).</p>
        )}
      </Card>

      <Card title="Red">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Puerto del contenedor" hint="Donde escucha tu app (variable PORT).">
            <input required type="number" min={1} max={65535} value={f.containerPort} onChange={set('containerPort')} className={inputClass} />
          </Field>
          <Field label="Puerto publicado en el servidor" hint="Vacío = no se expone.">
            <input type="number" min={1} max={65535} value={f.hostPort} onChange={set('hostPort')} placeholder="Sin publicar" className={inputClass} />
          </Field>
        </div>
      </Card>

      <Card title="Recursos">
        <div className="grid grid-cols-2 gap-3">
          <Field label="CPU máxima (núcleos)" hint={cores ? `El servidor tiene ${cores} núcleo${cores === 1 ? '' : 's'}. Ej. 0.5 = medio núcleo.` : 'Ej. 0.5 = medio núcleo.'}>
            <input type="number" min={0.01} max={cores || 256} step={0.01} value={f.cpuLimit} onChange={set('cpuLimit')} placeholder="Sin límite" className={inputClass} />
          </Field>
          <Field
            label="Memoria máxima (MB)"
            hint={memTotal ? `El servidor tiene ${formatBytes(memTotal)}. Si la app lo supera, Docker la reinicia.` : 'Si la app lo supera, Docker la reinicia.'}
          >
            <input type="number" min={32} step={1} value={f.memoryLimitMb} onChange={set('memoryLimitMb')} placeholder="Sin límite" className={inputClass} />
          </Field>
        </div>
      </Card>

      {update.isError && <ErrorBox>{update.error.message}</ErrorBox>}

      <div className={`flex items-center gap-3 py-3 ${dirty ? 'sticky bottom-0 border-t border-zinc-800 bg-zinc-950/90 backdrop-blur' : ''}`}>
        <button
          type="submit"
          disabled={!dirty || update.isPending}
          className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50"
        >
          {update.isPending ? 'Guardando…' : 'Guardar cambios'}
        </button>
        {dirty && (
          <button
            type="button"
            onClick={() => {
              setF(initial)
              setBuild(savedBuild(p))
            }}
            className="text-sm text-zinc-400 hover:text-zinc-200"
          >
            Descartar
          </button>
        )}
        <span className="text-xs text-zinc-500">
          {!dirty && update.isSuccess ? (
            <>
              <span className="mr-1 text-[#0ca30c]">✓</span>Guardado. Se aplica en el próximo deploy.
            </>
          ) : (
            'Los cambios se aplican en el próximo deploy.'
          )}
        </span>
      </div>
    </form>
  )
}

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-4 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <h2 className="text-sm font-medium">{title}</h2>
      {children}
    </section>
  )
}
