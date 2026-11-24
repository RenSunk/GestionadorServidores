import { useState } from 'react'
import type { BuildType, Runtime } from '../lib/projects.ts'
import { inputClass, monoInput } from '../lib/ui.ts'
import { DockerfilePreview } from './DockerfilePreview.tsx'
import { Choice, Field } from './Form.tsx'

export type Build = {
  type: BuildType
  runtime: string
  version: string
  baseImage: string
  buildCommand: string
  startCommand: string
}

/** Lenguaje y comandos a los que se puede volver (los detectados o los guardados). */
export type Suggestion = Omit<Build, 'type'> & { label: string }

type Props = {
  build: Build
  setBuild: (b: Build) => void
  suggestion?: Suggestion
  /** undefined = no se sabe (no se analizó el repo). */
  hasDockerfile?: boolean
  runtimes: Runtime[]
  port: number
  onRuntimeChange?: (rt: Runtime) => void
  dockerfilePath: string
  /** Variables de build del proyecto (se declaran con ARG en el Dockerfile). */
  buildArgs?: string[]
}

export function BuildConfig({ build, setBuild, suggestion, hasDockerfile, runtimes, port, onRuntimeChange, dockerfilePath, buildArgs }: Props) {
  const [showPreview, setShowPreview] = useState(false)
  const rt = runtimes.find((r) => r.id === build.runtime)

  function selectRuntime(id: string) {
    const next = runtimes.find((r) => r.id === id)
    if (!next) return
    // Si vuelve al lenguaje sugerido, recupera sus comandos; si no, quedan vacíos con ejemplos.
    const s = suggestion?.runtime === id ? suggestion : null
    setBuild({
      ...build,
      runtime: id,
      version: s ? s.version : next.defaultVersion,
      baseImage: s ? s.baseImage : '',
      buildCommand: s ? s.buildCommand : '',
      startCommand: s ? s.startCommand : '',
    })
    onRuntimeChange?.(next)
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-2 sm:grid-cols-2">
        <Choice
          checked={build.type === 'dockerfile'}
          disabled={hasDockerfile === false && build.type !== 'dockerfile'}
          onChange={() => setBuild({ ...build, type: 'dockerfile' })}
          title="Usar el Dockerfile del repositorio"
          hint={hasDockerfile === false ? `No hay ${dockerfilePath} en el repositorio` : dockerfilePath}
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
                    {suggestion && r.id === suggestion.runtime ? ` (${suggestion.label})` : ''}
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
                      buildArgs={buildArgs}
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
