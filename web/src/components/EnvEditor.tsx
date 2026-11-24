import { useState } from 'react'
import { type EnvVar, type ProjectDetail, useEnv, useSetEnv } from '../lib/projects.ts'
import { ErrorBox } from './Form.tsx'

const cell =
  'w-full rounded-md border border-zinc-700 bg-zinc-900 px-2.5 py-1.5 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none'

// Variables que los frameworks leen al compilar el frontend: se sugieren como "build".
const BUILD_PREFIXES = /^(NEXT_PUBLIC_|VITE_|REACT_APP_|NUXT_PUBLIC_|PUBLIC_|EXPO_PUBLIC_)/

type Row = EnvVar & { id: number }

export function EnvEditor({ project }: { project: ProjectDetail }) {
  const { data, isPending, error } = useEnv(project.id)
  const save = useSetEnv(project.id) // fuera del formulario: el "Guardado" sobrevive al reinicio
  if (isPending) return <p className="text-sm text-zinc-500">Cargando…</p>
  if (error) return <ErrorBox>{error.message}</ErrorBox>
  // key: al guardar llegan los datos nuevos y el formulario se reinicia con ellos.
  return <EnvForm key={JSON.stringify(data)} project={project} initial={data} save={save} />
}

let nextId = 1
const toRows = (vars: EnvVar[]): Row[] => vars.map((v) => ({ ...v, id: nextId++ }))
const emptyRow = (): Row => ({ key: '', value: '', build: false, id: nextId++ })

function EnvForm({ project, initial, save }: { project: ProjectDetail; initial: EnvVar[]; save: ReturnType<typeof useSetEnv> }) {
  const [rows, setRows] = useState<Row[]>(() => (initial.length ? toRows(initial) : [emptyRow()]))
  const [reveal, setReveal] = useState(false)
  const [pasting, setPasting] = useState(false)
  const [pasteText, setPasteText] = useState('')

  const clean = (rs: Row[]) => rs.filter((r) => r.key || r.value).map(({ key, value, build }) => ({ key: key.trim(), value, build }))
  const dirty = JSON.stringify(clean(rows)) !== JSON.stringify(initial)

  function update(id: number, patch: Partial<Row>) {
    setRows((rs) =>
      rs.map((r) => {
        if (r.id !== id) return r
        const next = { ...r, ...patch }
        // Al escribir un NEXT_PUBLIC_… por primera vez, se marca como variable de build.
        if (patch.key !== undefined && !BUILD_PREFIXES.test(r.key) && BUILD_PREFIXES.test(next.key)) next.build = true
        return next
      }),
    )
  }

  function applyPaste() {
    const parsed = parseDotenv(pasteText)
    setRows((rs) => {
      const byKey = new Map(clean(rs).map((v) => [v.key, v]))
      for (const v of parsed) byKey.set(v.key, { ...v, build: byKey.get(v.key)?.build ?? BUILD_PREFIXES.test(v.key) })
      return toRows([...byKey.values()])
    })
    setPasteText('')
    setPasting(false)
  }

  const duplicated = new Set(
    clean(rows)
      .map((v) => v.key)
      .filter((k, i, all) => k && all.indexOf(k) !== i),
  )

  return (
    <section className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium">Variables de entorno</h2>
          <p className="mt-1 max-w-xl text-xs text-zinc-500">
            Se guardan cifradas y llegan al contenedor al desplegar. <span className="text-zinc-400">PORT</span> la define Gestionador.
          </p>
        </div>
        <div className="flex gap-3 text-sm">
          <button type="button" onClick={() => setReveal((v) => !v)} className="text-zinc-400 hover:text-zinc-200">
            {reveal ? 'Ocultar valores' : 'Mostrar valores'}
          </button>
          <button type="button" onClick={() => setPasting((v) => !v)} className="text-zinc-400 hover:text-zinc-200">
            Pegar .env
          </button>
        </div>
      </div>

      {pasting && (
        <div className="mt-4">
          <textarea
            value={pasteText}
            onChange={(e) => setPasteText(e.target.value)}
            rows={6}
            autoFocus
            placeholder={'DATABASE_URL=postgres://…\nAPI_KEY="abc123"\n# los comentarios se ignoran'}
            className={`${cell} resize-y`}
          />
          <div className="mt-2 flex gap-3">
            <button type="button" onClick={applyPaste} disabled={!pasteText.trim()} className="rounded-md bg-zinc-100 px-3 py-1.5 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50">
              Agregar
            </button>
            <button type="button" onClick={() => setPasting(false)} className="text-sm text-zinc-400 hover:text-zinc-200">
              Cancelar
            </button>
          </div>
          <p className="mt-1 text-xs text-zinc-500">Las variables que ya existen se reemplazan con el valor pegado.</p>
        </div>
      )}

      <div className="mt-4 space-y-2">
        <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_4.5rem_2rem] gap-2 text-xs text-zinc-500">
          <span>Nombre</span>
          <span>Valor</span>
          <span title="Disponible también al construir la imagen">En build</span>
          <span />
        </div>
        {rows.map((r) => (
          <div key={r.id} className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_4.5rem_2rem] items-center gap-2">
            <input
              value={r.key}
              onChange={(e) => update(r.id, { key: e.target.value.replace(/\s/g, '') })}
              placeholder="NOMBRE"
              aria-label="Nombre de la variable"
              className={`${cell} ${duplicated.has(r.key) ? 'border-[#d03b3b]' : ''}`}
            />
            <input
              type={reveal ? 'text' : 'password'}
              value={r.value}
              onChange={(e) => update(r.id, { value: e.target.value })}
              placeholder="valor"
              aria-label={`Valor de ${r.key || 'la variable'}`}
              autoComplete="off"
              className={cell}
            />
            <label className="flex justify-center">
              <input
                type="checkbox"
                checked={r.build}
                onChange={(e) => update(r.id, { build: e.target.checked })}
                aria-label={`${r.key || 'Variable'} disponible en build`}
                className="h-4 w-4 accent-zinc-200"
              />
            </label>
            <button
              type="button"
              onClick={() => setRows((rs) => (rs.length > 1 ? rs.filter((x) => x.id !== r.id) : [emptyRow()]))}
              aria-label={`Eliminar ${r.key || 'variable'}`}
              className="rounded-md py-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"
            >
              ✕
            </button>
          </div>
        ))}
        <button type="button" onClick={() => setRows((rs) => [...rs, emptyRow()])} className="text-sm text-zinc-400 hover:text-zinc-200">
          + Agregar variable
        </button>
      </div>

      {rows.some((r) => r.build) && (
        <p className="mt-3 rounded-md border border-[#fab219]/30 bg-[#fab219]/5 p-2.5 text-xs text-zinc-300">
          <span className="mr-1.5 text-[#fab219]">▲</span>
          Las variables “en build” quedan guardadas en la imagen: no marques contraseñas ni claves privadas.
          {project.buildType === 'dockerfile' && (
            <>
              {' '}
              Con tu propio Dockerfile, decláralas con <code className="font-mono">ARG NOMBRE</code> antes de usarlas.
            </>
          )}
        </p>
      )}

      {duplicated.size > 0 && <ErrorBox>Hay nombres repetidos: {[...duplicated].join(', ')}</ErrorBox>}
      {save.isError && <ErrorBox>{save.error.message}</ErrorBox>}

      <div className="mt-4 flex items-center gap-3">
        <button
          type="button"
          onClick={() => save.mutate(clean(rows))}
          disabled={!dirty || save.isPending || duplicated.size > 0}
          className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50"
        >
          {save.isPending ? 'Guardando…' : 'Guardar variables'}
        </button>
        {dirty && (
          <button type="button" onClick={() => setRows(initial.length ? toRows(initial) : [emptyRow()])} className="text-sm text-zinc-400 hover:text-zinc-200">
            Descartar cambios
          </button>
        )}
        {!dirty && save.isSuccess && <span className="text-sm text-zinc-400"><span className="mr-1 text-[#0ca30c]">✓</span>Guardado</span>}
      </div>
    </section>
  )
}

/** Interpreta un archivo .env: KEY=valor, comillas opcionales, `export` y comentarios. */
function parseDotenv(text: string): EnvVar[] {
  const out: EnvVar[] = []
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue
    const m = line.match(/^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/)
    if (!m) continue
    let value = m[2]
    const q = value[0]
    if ((q === '"' || q === "'") && value.lastIndexOf(q) > 0) {
      value = value.slice(1, value.lastIndexOf(q))
    } else {
      value = value.replace(/\s+#.*$/, '') // comentario al final de la línea
    }
    out.push({ key: m[1], value, build: false })
  }
  return out
}
