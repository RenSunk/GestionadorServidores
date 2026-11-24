import { type FormEvent, type ReactNode, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { DockerfilePreview } from '../components/DockerfilePreview.tsx'
import {
  type BuildType,
  type Detection,
  type Runtime,
  useCreateProject,
  useDetect,
  useRuntimes,
} from '../lib/projects.ts'
import { useServers } from '../lib/servers.ts'

const inputClass =
  'mt-1 w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none disabled:opacity-50'
const monoInput = `${inputClass} font-mono text-xs`

type Build = {
  type: BuildType
  runtime: string
  version: string
  baseImage: string
  buildCommand: string
  startCommand: string
}

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
              analysis={analysis}
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

type BuildConfigProps = {
  build: Build
  setBuild: (b: Build) => void
  analysis: Detection
  runtimes: Runtime[]
  port: number
  onRuntimeChange: (rt: Runtime) => void
  dockerfilePath: string
}

function BuildConfig({ build, setBuild, analysis, runtimes, port, onRuntimeChange, dockerfilePath }: BuildConfigProps) {
  const [showPreview, setShowPreview] = useState(false)
  const rt = runtimes.find((r) => r.id === build.runtime)

  function selectRuntime(id: string) {
    const next = runtimes.find((r) => r.id === id)
    if (!next) return
    // Si vuelve al lenguaje detectado, recupera los comandos sugeridos; si no, quedan vacíos con ejemplos.
    const isDetected = id === analysis.runtime
    setBuild({
      ...build,
      runtime: id,
      version: isDetected ? analysis.version : next.defaultVersion,
      baseImage: '',
      buildCommand: isDetected ? analysis.buildCommand : '',
      startCommand: isDetected ? analysis.startCommand : '',
    })
    onRuntimeChange(next)
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-2 sm:grid-cols-2">
        <Choice
          checked={build.type === 'dockerfile'}
          disabled={!analysis.hasDockerfile}
          onChange={() => setBuild({ ...build, type: 'dockerfile' })}
          title="Usar el Dockerfile del repositorio"
          hint={analysis.hasDockerfile ? dockerfilePath : `No hay ${dockerfilePath} en el repositorio`}
        />
        <Choice
          checked={build.type === 'runtime'}
          onChange={() => setBuild({ ...build, type: 'runtime' })}
          title="Elegir lenguaje y comandos"
          hint="Generamos el Dockerfile automáticamente"
        />
      </div>

      {build.type === 'runtime' && (
        <div className="space-y-4">
          <div className="grid grid-cols-[1fr_10rem] gap-3">
            <Field label="Lenguaje">
              <select required value={build.runtime} onChange={(e) => selectRuntime(e.target.value)} className={inputClass}>
                {!build.runtime && <option value="">Elige un lenguaje…</option>}
                {runtimes.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label}
                    {r.id === analysis.runtime ? ' (detectado)' : ''}
                  </option>
                ))}
              </select>
            </Field>
            {rt && !rt.needsBaseImage && (
              <Field label="Versión">
                <input
                  required
                  list={`versions-${rt.id}`}
                  value={build.version}
                  onChange={(e) => setBuild({ ...build, version: e.target.value })}
                  className={inputClass}
                />
                <datalist id={`versions-${rt.id}`}>
                  {rt.versions?.map((v) => (
                    <option key={v} value={v} />
                  ))}
                </datalist>
              </Field>
            )}
          </div>

          {rt?.needsBaseImage && (
            <Field label="Imagen base de Docker" hint="Cualquier imagen pública: php:8.3-cli, ruby:3.3-slim, mcr.microsoft.com/dotnet/sdk:8.0…">
              <input
                required
                value={build.baseImage}
                onChange={(e) => setBuild({ ...build, baseImage: e.target.value })}
                placeholder="ruby:3.3-slim"
                className={monoInput}
              />
            </Field>
          )}

          {rt && (
            <>
              <Field label="Comando de build" hint="Se ejecuta al construir la imagen. Opcional. Encadena pasos con &&.">
                <input
                  value={build.buildCommand}
                  onChange={(e) => setBuild({ ...build, buildCommand: e.target.value })}
                  placeholder={rt.buildPlaceholder}
                  className={monoInput}
                />
              </Field>
              <Field label="Comando de inicio" hint="Arranca tu app. Debe escuchar en 0.0.0.0 y en el puerto $PORT.">
                <input
                  required
                  value={build.startCommand}
                  onChange={(e) => setBuild({ ...build, startCommand: e.target.value })}
                  placeholder={rt.startPlaceholder}
                  className={monoInput}
                />
              </Field>

              <div>
                <button type="button" onClick={() => setShowPreview((v) => !v)} className="text-sm text-zinc-400 hover:text-zinc-200">
                  {showPreview ? '▾' : '▸'} Ver Dockerfile generado
                </button>
                {showPreview && (
                  <div className="mt-2">
                    <DockerfilePreview
                      spec={{
                        runtime: build.runtime,
                        version: build.version,
                        baseImage: build.baseImage,
                        buildCommand: build.buildCommand,
                        startCommand: build.startCommand,
                        port,
                      }}
                    />
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function Choice(props: { checked: boolean; disabled?: boolean; onChange: () => void; title: string; hint: string }) {
  return (
    <label
      className={`rounded-md border p-3 text-sm ${
        props.disabled
          ? 'cursor-not-allowed border-zinc-800 opacity-50'
          : props.checked
            ? 'cursor-pointer border-zinc-400 bg-zinc-900'
            : 'cursor-pointer border-zinc-800 hover:border-zinc-700'
      }`}
    >
      <input type="radio" checked={props.checked} disabled={props.disabled} onChange={props.onChange} className="sr-only" />
      <div className="font-medium">{props.title}</div>
      <div className="mt-1 text-xs text-zinc-500">{props.hint}</div>
    </label>
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

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="text-sm text-zinc-300">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-zinc-500">{hint}</span>}
    </label>
  )
}

function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <div className="mt-3 rounded-md border border-[#d03b3b]/50 bg-[#d03b3b]/10 p-3 text-sm text-zinc-200">
      <span className="mr-1.5 text-[#d03b3b]">✕</span>
      {children}
    </div>
  )
}

/** "https://github.com/user/mi-api.git" → "mi-api" */
function repoSlug(url: string) {
  return url.replace(/\.git$/, '').split('/').filter(Boolean).pop() ?? ''
}
