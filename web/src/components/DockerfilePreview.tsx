import { useEffect, useState } from 'react'
import { type BuildSpec, useDockerfilePreview, useProjectDockerfile } from '../lib/projects.ts'

/** Vista previa del Dockerfile para una configuración que aún no se guarda. */
export function DockerfilePreview({ spec, buildArgs }: { spec: BuildSpec; buildArgs?: string[] }) {
  // Espera a que el usuario deje de escribir para no pedir una vista previa por tecla.
  const debounced = useDebounced({ ...spec, buildArgs: buildArgs ?? [] }, 400)
  const ready = !!debounced.runtime && !!debounced.startCommand
  const { data, error, isFetching } = useDockerfilePreview(debounced, ready)

  if (!ready) return <Box muted>Completa el lenguaje y el comando de inicio para ver el Dockerfile.</Box>
  if (error) return <Box muted>{error.message}</Box>
  return <Box dimmed={isFetching}>{data?.dockerfile ?? 'Generando…'}</Box>
}

/** Dockerfile generado de un proyecto ya guardado. */
export function ProjectDockerfile({ projectId }: { projectId: string }) {
  const { data, error } = useProjectDockerfile(projectId, true)
  if (error) return <Box muted>{error.message}</Box>
  return <Box>{data?.dockerfile ?? 'Cargando…'}</Box>
}

function Box({ children, muted, dimmed }: { children: string; muted?: boolean; dimmed?: boolean }) {
  return (
    <pre
      className={`overflow-x-auto rounded-md border border-zinc-800 bg-zinc-950 p-3 font-mono text-xs leading-relaxed ${
        muted ? 'text-zinc-500' : 'text-zinc-300'
      } ${dimmed ? 'opacity-60' : ''}`}
    >
      {children}
    </pre>
  )
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  const key = JSON.stringify(value)
  useEffect(() => {
    const t = setTimeout(() => setV(JSON.parse(key)), ms)
    return () => clearTimeout(t)
  }, [key, ms])
  return v
}
