import { type FormEvent, type ReactNode, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { type Build, BuildConfig } from '../components/BuildConfig.tsx'
import { ErrorBox, Field } from '../components/Form.tsx'
import {
  type Detection,
  type Runtime,
  useCreateProject,
  useDetect,
  useRuntimes,
} from '../lib/projects.ts'
import { useServers } from '../lib/servers.ts'
import { inputClass } from '../lib/ui.ts'

export function NewProjectPage() {
  const navigate = useNavigate()
  const servers = useServers()
  const runtimes = useRuntimes()
  const detect = useDetect()
  const create = useCreateProject()

  const [repo, setRepo] = useState({ repoUrl: '', branch: 'main', gitToken: '', gitUsername: '', rootDir: '', dockerfile: 'Dockerfile' })
  const [name, setName] = useState('')
  const [nameTouched, setNameTouched] = useState(false) // si el usuario lo editó, no lo pisamos
  const [serverId, setServerId] = useState('')
  const [ports, setPorts] = useState({ containerPort: '3000', hostPort: '' })
  const [showAdvanced, setShowAdvanced] = useState(false)
  // Resultado del análisis. Se descarta si cambia algo del repositorio.
  const [analysis, setAnalysis] = useState<Detection | null>(null)
  const [build, setBuild] = useState<Build | null>(null)

  const selectedServer = serverId || servers.data?.[0]?.id || ''

  const setRepoField = (field: keyof typeof repo) => (e: { target: { value: string } }) => {
    setRepo((r) => ({ ...r, [field]: e.target.value }))
    setAnalysis(null)
    setBuild(null)
    detect.reset()
  }

  function analyze() {
    detect.mutate(
      {
        repoUrl: repo.repoUrl,
        branch: repo.branch,
        gitToken: repo.gitToken || undefined,
        gitUsername: repo.gitUsername || undefined,
        rootDir: repo.rootDir || undefined,
        dockerfile: repo.dockerfile || undefined,
      },
      {
        onSuccess: (d) => {
          setAnalysis(d)
          const rt = runtimes.data?.find((r) => r.id === d.runtime)
          setBuild({
            // Si el repo trae Dockerfile, se usa por defecto; si no, se genera.
            type: d.hasDockerfile ? 'dockerfile' : 'runtime',
            runtime: d.runtime,
            version: d.version || rt?.defaultVersion || '',
            baseImage: '',
            buildCommand: d.buildCommand,
            startCommand: d.startCommand,
          })
          if (d.port) setPorts((p) => ({ ...p, containerPort: String(d.port) }))
          if (!nameTouched) setName(repoSlug(repo.repoUrl))
        },
      },
    )
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!build) return
    create.mutate(
      {
        name,
        serverId: selectedServer,
        repoUrl: repo.repoUrl,
        branch: repo.branch,
        gitToken: repo.gitToken || undefined,
        gitUsername: repo.gitUsername || undefined,
        rootDir: repo.rootDir || undefined,
        dockerfile: repo.dockerfile || undefined,
        containerPort: Number(ports.containerPort),
        hostPort: ports.hostPort ? Number(ports.hostPort) : null,
        buildType: build.type,
        ...(build.type === 'runtime' && {
          runtime: build.runtime,
          runtimeVersion: build.version,
          baseImage: build.baseImage,
          buildCommand: build.buildCommand,
          startCommand: build.startCommand,
        }),
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
        Conecta un repositorio. Si no tiene Dockerfile, elige el lenguaje y los comandos y lo generamos por ti.
      </p>

      <form onSubmit={onSubmit} className="mt-8 space-y-8">
        {/* 1. Repositorio */}
        <Section step={1} title="Repositorio">
          <div className="grid grid-cols-[1fr_9rem] gap-3">
            <Field label="URL del repositorio (HTTPS)">
              <input
                required
                type="url"
                value={repo.repoUrl}
                onChange={setRepoField('repoUrl')}
                placeholder="https://github.com/usuario/repo.git"
                className={inputClass}
              />
            </Field>
            <Field label="Rama">
              <input required value={repo.branch} onChange={setRepoField('branch')} className={inputClass} />
            </Field>
          </div>

          <Field label="Token de acceso" hint="Solo para repos privados (permiso de lectura). Se guarda cifrado.">
            <input
              type="password"
              value={repo.gitToken}
              onChange={setRepoField('gitToken')}
              autoComplete="off"
              placeholder="ghp_… / glpat-…"
              className={inputClass}
            />
          </Field>

          <div>
            <button type="button" onClick={() => setShowAdvanced((v) => !v)} className="text-sm text-zinc-400 hover:text-zinc-200">
              {showAdvanced ? '▾' : '▸'} Opciones avanzadas
            </button>
            {showAdvanced && (
              <div className="mt-4 space-y-4 border-l border-zinc-800 pl-4">
                <div className="grid grid-cols-2 gap-3">
                  <Field label="Carpeta raíz" hint="Para monorepos. Vacío = raíz del repo.">
                    <input value={repo.rootDir} onChange={setRepoField('rootDir')} placeholder="apps/api" className={inputClass} />
                  </Field>
                  <Field label="Ruta del Dockerfile" hint="Relativa a la carpeta raíz.">
                    <input value={repo.dockerfile} onChange={setRepoField('dockerfile')} className={inputClass} />
                  </Field>
                </div>
                <Field label="Usuario de Git" hint="Solo lo exige Bitbucket. GitHub, GitLab y Gitea funcionan con el token solo.">
                  <input value={repo.gitUsername} onChange={setRepoField('gitUsername')} className={inputClass} />
                </Field>
              </div>
            )}
          </div>

          {!analysis && (
            <div>
              <button
                type="button"
                onClick={analyze}
                disabled={!repo.repoUrl || !repo.branch || detect.isPending}
                className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50"
              >
                {detect.isPending ? 'Analizando repositorio…' : 'Analizar repositorio'}
              </button>
              <p className="mt-2 text-xs text-zinc-500">Clonamos el último commit para detectar el lenguaje y verificar el acceso.</p>
              {detect.isError && <ErrorBox>{detect.error.message}</ErrorBox>}
            </div>
          )}
        </Section>

        {/* 2. Build */}
        {analysis && build && runtimes.data && (
          <Section step={2} title="Build">
            <AnalysisSummary analysis={analysis} runtimes={runtimes.data} dockerfilePath={repo.dockerfile || 'Dockerfile'} />
            <BuildConfig
              build={build}
              setBuild={setBuild}
              hasDockerfile={analysis.hasDockerfile}
              suggestion={
                analysis.runtime
                  ? { runtime: analysis.runtime, version: analysis.version, baseImage: '', buildCommand: analysis.buildCommand, startCommand: analysis.startCommand, label: 'detectado' }
                  : undefined
              }
              runtimes={runtimes.data}
              port={Number(ports.containerPort) || 0}
              onRuntimeChange={(rt) => setPorts((p) => ({ ...p, containerPort: String(rt.defaultPort) }))}
              dockerfilePath={repo.dockerfile || 'Dockerfile'}
            />
          </Section>
        )}

        {/* 3. Despliegue */}
        {analysis && build && (
          <Section step={3} title="Despliegue">
            <div className="grid grid-cols-2 gap-3">
              <Field label="Nombre del proyecto">
                <input
                  required
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value)
                    setNameTouched(true)
                  }}
                  placeholder="mi-api"
                  className={inputClass}
                />
              </Field>
              <Field label="Servidor">
                <select required value={selectedServer} onChange={(e) => setServerId(e.target.value)} className={inputClass}>
                  {servers.data?.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                      {s.system.dockerStatus !== 'running' ? ' (sin Docker)' : ''}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Puerto del contenedor" hint="Donde escucha tu app. Se pasa como variable PORT.">
                <input
                  required
                  type="number"
                  min={1}
                  max={65535}
                  value={ports.containerPort}
                  onChange={(e) => setPorts((p) => ({ ...p, containerPort: e.target.value }))}
                  className={inputClass}
                />
              </Field>
              <Field label="Puerto publicado en el servidor" hint="Opcional. Vacío = no se expone (los dominios llegan en la fase 4).">
                <input
                  type="number"
                  min={1}
                  max={65535}
                  value={ports.hostPort}
                  onChange={(e) => setPorts((p) => ({ ...p, hostPort: e.target.value }))}
                  placeholder="8080"
                  className={inputClass}
                />
              </Field>
            </div>

            {create.isError && <ErrorBox>{create.error.message}</ErrorBox>}

            <div className="flex items-center gap-3">
              <button
                type="submit"
                disabled={create.isPending}
                className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-60"
              >
                {create.isPending ? 'Creando…' : 'Crear proyecto'}
              </button>
              <Link to="/projects" className="text-sm text-zinc-400 hover:text-zinc-200">
                Cancelar
              </Link>
            </div>
          </Section>
        )}
      </form>
    </div>
  )
}

function AnalysisSummary({ analysis: a, runtimes, dockerfilePath }: { analysis: Detection; runtimes: Runtime[]; dockerfilePath: string }) {
  const rt = runtimes.find((r) => r.id === a.runtime)
  return (
    <div className="space-y-2 rounded-md border border-zinc-800 bg-zinc-900/50 p-3 text-sm">
      {a.hasDockerfile && (
        <div>
          <span className="mr-1.5 text-[#0ca30c]">●</span>
          Se encontró <span className="font-mono text-xs">{dockerfilePath}</span> en el repositorio.
        </div>
      )}
      {rt ? (
        <div>
          <span className="mr-1.5 text-[#0ca30c]">●</span>
          Detectado: <span className="font-medium">{a.framework}</span>
          <span className="text-zinc-400">
            {' '}
            · {rt.label} {a.version}
            {a.packageManager && rt.id === 'node' ? ` · ${a.packageManager}` : ''}
          </span>
        </div>
      ) : (
        <div>
          <span className="mr-1.5 text-[#fab219]">▲</span>
          No se pudo detectar el lenguaje. {a.hasDockerfile ? 'Puedes usar el Dockerfile o elegir' : 'Elige'} uno de la lista e
          indica los comandos.
        </div>
      )}
      {a.notes.map((n) => (
        <div key={n} className="text-xs text-zinc-400">
          <span className="mr-1.5 text-[#fab219]">▲</span>
          {n}
        </div>
      ))}
    </div>
  )
}

function Section({ step, title, children }: { step: number; title: string; children: ReactNode }) {
  return (
    <section className="space-y-4">
      <h2 className="flex items-center gap-2 text-sm font-medium text-zinc-200">
        <span className="flex h-5 w-5 items-center justify-center rounded-full border border-zinc-600 text-[11px] text-zinc-400">{step}</span>
        {title}
      </h2>
      {children}
    </section>
  )
}



/** "https://github.com/user/mi-api.git" → "mi-api" */
function repoSlug(url: string) {
  return url.replace(/\.git$/, '').split('/').filter(Boolean).pop() ?? ''
}
