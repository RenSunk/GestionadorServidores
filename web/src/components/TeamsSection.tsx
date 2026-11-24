import { type FormEvent, useState } from 'react'
import { useUsers } from '../lib/auth.ts'
import { useProjects } from '../lib/projects.ts'
import { useServers } from '../lib/servers.ts'
import { type Team, type TeamInput, useDeleteTeam, useSaveTeam, useTeams } from '../lib/teams.ts'
import { inputClass } from '../lib/ui.ts'
import { ErrorBox } from './Form.tsx'

const primary = 'rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50'

export function TeamsSection() {
  const { data: teams, isPending, error } = useTeams()
  const [creating, setCreating] = useState(false)

  return (
    <section className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium">Equipos</h2>
          <p className="mt-1 max-w-2xl text-xs text-zinc-500">
            Limitan qué proyectos y servidores ve cada persona; el rol sigue definiendo qué puede hacer. Un proyecto o servidor sin equipos
            es visible para todos. Al asignarlo a uno o más equipos, solo lo ven sus miembros y los administradores.
          </p>
        </div>
        {!creating && (
          <button onClick={() => setCreating(true)} className={primary}>
            Nuevo equipo
          </button>
        )}
      </div>
      {isPending && <p className="mt-3 text-sm text-zinc-500">Cargando…</p>}
      {error && <ErrorBox>{error.message}</ErrorBox>}
      <div className="mt-4 space-y-3">
        {creating && <TeamEditor onDone={() => setCreating(false)} />}
        {teams?.map((t) => <TeamCard key={t.id} team={t} />)}
        {teams?.length === 0 && !creating && <p className="text-sm text-zinc-500">Todavía no hay equipos: todos ven todos los proyectos y servidores.</p>}
      </div>
    </section>
  )
}

function TeamCard({ team: t }: { team: Team }) {
  const [editing, setEditing] = useState(false)
  const del = useDeleteTeam()
  const { data: users } = useUsers()
  const { data: projects } = useProjects()
  const { data: servers } = useServers()
  const names = <T extends { id: string; name: string }>(list: T[] | undefined, ids: string[]) =>
    ids.map((id) => list?.find((x) => x.id === id)?.name ?? '…').join(', ') || '—'

  if (editing) return <TeamEditor team={t} onDone={() => setEditing(false)} />

  function onDelete() {
    if (!confirm(`¿Eliminar el equipo "${t.name}"? Sus proyectos y servidores que no estén en otro equipo pasarán a ser visibles para todos.`)) return
    del.mutate(t.id)
  }

  return (
    <div className="rounded-md border border-zinc-800 p-3 text-sm">
      <div className="flex items-center justify-between gap-3">
        <span className="font-medium text-zinc-100">{t.name}</span>
        <div className="flex gap-3 text-xs">
          <button onClick={() => setEditing(true)} className="text-zinc-400 hover:text-zinc-200">
            Editar
          </button>
          <button onClick={onDelete} disabled={del.isPending} className="text-zinc-400 hover:text-zinc-200 disabled:opacity-50">
            Eliminar
          </button>
        </div>
      </div>
      <dl className="mt-2 grid gap-1 text-xs sm:grid-cols-[6rem_1fr]">
        <dt className="text-zinc-500">Miembros</dt>
        <dd className="text-zinc-300">{names(users, t.userIds)}</dd>
        <dt className="text-zinc-500">Proyectos</dt>
        <dd className="text-zinc-300">{names(projects, t.projectIds)}</dd>
        <dt className="text-zinc-500">Servidores</dt>
        <dd className="text-zinc-300">{names(servers, t.serverIds)}</dd>
      </dl>
      {del.isError && <p className="mt-2 text-xs text-red-400">{del.error.message}</p>}
    </div>
  )
}

function TeamEditor({ team, onDone }: { team?: Team; onDone: () => void }) {
  const save = useSaveTeam()
  const { data: users } = useUsers()
  const { data: projects } = useProjects()
  const { data: servers } = useServers()
  const [v, setV] = useState<TeamInput>({
    name: team?.name ?? '',
    userIds: team?.userIds ?? [],
    projectIds: team?.projectIds ?? [],
    serverIds: team?.serverIds ?? [],
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    save.mutate({ id: team?.id, ...v }, { onSuccess: onDone })
  }

  const members = (users ?? []).filter((u) => u.role !== 'admin')
  return (
    <form onSubmit={onSubmit} className="rounded-md border border-zinc-600 bg-zinc-900 p-3">
      <input
        required
        autoFocus
        value={v.name}
        onChange={(e) => setV({ ...v, name: e.target.value })}
        placeholder="Nombre del equipo (ej. Pagos)"
        aria-label="Nombre del equipo"
        className={`${inputClass} mt-0`}
      />
      <div className="mt-3 grid gap-4 sm:grid-cols-3">
        <Checklist
          title="Miembros"
          hint="Los administradores ven todo y no necesitan equipo."
          items={members.map((u) => ({ id: u.id, label: u.name, sub: u.email }))}
          selected={v.userIds}
          onChange={(userIds) => setV({ ...v, userIds })}
        />
        <Checklist
          title="Proyectos"
          items={(projects ?? []).map((p) => ({ id: p.id, label: p.name, sub: p.serverName }))}
          selected={v.projectIds}
          onChange={(projectIds) => setV({ ...v, projectIds })}
        />
        <Checklist
          title="Servidores"
          hint="Para crear proyectos en ellos."
          items={(servers ?? []).map((s) => ({ id: s.id, label: s.name, sub: s.host }))}
          selected={v.serverIds}
          onChange={(serverIds) => setV({ ...v, serverIds })}
        />
      </div>
      {save.isError && <ErrorBox>{save.error.message}</ErrorBox>}
      <div className="mt-3 flex gap-3">
        <button type="submit" disabled={save.isPending} className={primary}>
          {save.isPending ? 'Guardando…' : team ? 'Guardar equipo' : 'Crear equipo'}
        </button>
        <button type="button" onClick={onDone} className="text-sm text-zinc-400 hover:text-zinc-200">
          Cancelar
        </button>
      </div>
    </form>
  )
}

function Checklist(props: {
  title: string
  hint?: string
  items: { id: string; label: string; sub?: string }[]
  selected: string[]
  onChange: (ids: string[]) => void
}) {
  const toggle = (id: string) =>
    props.onChange(props.selected.includes(id) ? props.selected.filter((x) => x !== id) : [...props.selected, id])
  return (
    <fieldset>
      <legend className="text-xs text-zinc-400">{props.title}</legend>
      {props.hint && <p className="text-[11px] text-zinc-600">{props.hint}</p>}
      <div className="mt-1.5 max-h-48 space-y-1 overflow-y-auto">
        {props.items.length === 0 && <p className="text-xs text-zinc-600">Nada para elegir.</p>}
        {props.items.map((it) => (
          <label key={it.id} className="flex cursor-pointer items-start gap-2 rounded px-1 py-0.5 text-xs hover:bg-zinc-800/60">
            <input type="checkbox" checked={props.selected.includes(it.id)} onChange={() => toggle(it.id)} className="mt-0.5 accent-zinc-200" />
            <span className="min-w-0">
              <span className="block truncate text-zinc-200">{it.label}</span>
              {it.sub && <span className="block truncate text-zinc-500">{it.sub}</span>}
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}
