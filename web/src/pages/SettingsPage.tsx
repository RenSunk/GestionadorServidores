import { type FormEvent, useState } from 'react'
import { AlertSettingsSection } from '../components/AlertSettingsSection.tsx'
import { ErrorBox, Field } from '../components/Form.tsx'
import { TeamsSection } from '../components/TeamsSection.tsx'
import {
  type Invite,
  ROLE_HINTS,
  ROLE_LABELS,
  type Role,
  type User,
  useCan,
  useChangePassword,
  useCreateInvite,
  useDeleteUser,
  useInvites,
  useMe,
  useRevokeInvite,
  useUpdateMe,
  useUpdateUser,
  useUsers,
} from '../lib/auth.ts'
import { formatRelative } from '../lib/format.ts'
import { inputClass } from '../lib/ui.ts'

const ROLES: Role[] = ['admin', 'developer', 'viewer']

const card = 'rounded-lg border border-zinc-800 bg-zinc-900/50 p-4'
const primary = 'rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50'

export function SettingsPage() {
  const can = useCan()
  return (
    <div className="max-w-4xl space-y-6">
      <h1 className="text-2xl font-semibold">Configuración</h1>
      <Account />
      {can('admin') && <Team />}
      {can('admin') && <TeamsSection />}
      {can('admin') && <AlertSettingsSection />}
    </div>
  )
}

// ---------- Mi cuenta ----------

function Account() {
  const { data: me } = useMe()
  if (!me) return null
  return (
    <section className={card}>
      <h2 className="text-sm font-medium">Mi cuenta</h2>
      <p className="mt-1 text-xs text-zinc-500">
        {me.email} · {ROLE_LABELS[me.role]}
      </p>
      <div className="mt-4 grid gap-6 md:grid-cols-2">
        <ProfileForm key={me.name} me={me} />
        <PasswordForm />
      </div>
    </section>
  )
}

function ProfileForm({ me }: { me: User }) {
  const update = useUpdateMe()
  const [name, setName] = useState(me.name)
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        update.mutate(name)
      }}
      className="space-y-3"
    >
      <Field label="Nombre">
        <input required value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
      </Field>
      {update.isError && <ErrorBox>{update.error.message}</ErrorBox>}
      <button type="submit" disabled={update.isPending || name.trim() === me.name} className={primary}>
        Guardar nombre
      </button>
    </form>
  )
}

function PasswordForm() {
  const change = useChangePassword()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [repeat, setRepeat] = useState('')
  const mismatch = repeat !== '' && repeat !== next

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (mismatch) return
    change.mutate(
      { current, new: next },
      {
        onSuccess: () => {
          setCurrent('')
          setNext('')
          setRepeat('')
        },
      },
    )
  }

  return (
    <form onSubmit={onSubmit} className="space-y-3">
      <Field label="Contraseña actual">
        <input
          type="password"
          required
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          className={inputClass}
        />
      </Field>
      <Field label="Contraseña nueva" hint="Al menos 10 caracteres. Se cerrarán tus otras sesiones.">
        <input
          type="password"
          required
          minLength={10}
          maxLength={72}
          autoComplete="new-password"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          className={inputClass}
        />
      </Field>
      <Field label="Repite la contraseña nueva">
        <input
          type="password"
          required
          autoComplete="new-password"
          value={repeat}
          onChange={(e) => setRepeat(e.target.value)}
          className={inputClass}
        />
      </Field>
      {mismatch && <ErrorBox>Las contraseñas no coinciden.</ErrorBox>}
      {change.isError && <ErrorBox>{change.error.message}</ErrorBox>}
      <div className="flex items-center gap-3">
        <button type="submit" disabled={change.isPending} className={primary}>
          Cambiar contraseña
        </button>
        {change.isSuccess && (
          <span className="text-sm text-zinc-400">
            <span className="mr-1 text-[#0ca30c]">✓</span>Contraseña cambiada
          </span>
        )}
      </div>
    </form>
  )
}

// ---------- Equipo (solo administradores) ----------

function Team() {
  const { data: users, isPending, error } = useUsers()
  const { data: me } = useMe()
  return (
    <section className={card}>
      <h2 className="text-sm font-medium">Usuarios</h2>
      <p className="mt-1 text-xs text-zinc-500">Quién puede entrar a Gestionador y qué puede hacer.</p>
      <InviteForm />
      <PendingInvites />
      {isPending && <p className="mt-4 text-sm text-zinc-500">Cargando…</p>}
      {error && <ErrorBox>{error.message}</ErrorBox>}
      <ul className="mt-4 divide-y divide-zinc-800 overflow-hidden rounded-md border border-zinc-800">
        {users?.map((u) => (
          <UserRow key={u.id} user={u} isMe={u.id === me?.id} />
        ))}
      </ul>
      <dl className="mt-4 grid gap-2 text-xs sm:grid-cols-3">
        {ROLES.map((r) => (
          <div key={r}>
            <dt className="text-zinc-300">{ROLE_LABELS[r]}</dt>
            <dd className="text-zinc-500">{ROLE_HINTS[r]}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function UserRow({ user: u, isMe }: { user: User; isMe: boolean }) {
  const update = useUpdateUser()
  const del = useDeleteUser()
  const error = update.error ?? del.error

  function onDelete() {
    if (!confirm(`¿Eliminar a ${u.name} (${u.email})? Perderá el acceso de inmediato.`)) return
    del.mutate(u.id)
  }

  return (
    <li className="px-4 py-3 text-sm">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className={u.disabled ? 'text-zinc-500 line-through' : 'text-zinc-200'}>
            {u.name} {isMe && <span className="text-xs text-zinc-500">(tú)</span>}
          </div>
          <div className="text-xs text-zinc-500">
            {u.email} · {u.lastLoginAt ? `entró ${formatRelative(u.lastLoginAt)}` : 'nunca entró'}
          </div>
        </div>
        {isMe ? (
          <span className="text-xs text-zinc-400">{ROLE_LABELS[u.role]}</span>
        ) : (
          <>
            <select
              value={u.role}
              disabled={update.isPending}
              onChange={(e) => update.mutate({ id: u.id, role: e.target.value as Role })}
              aria-label={`Rol de ${u.name}`}
              className="rounded-md border border-zinc-700 bg-zinc-900 px-2 py-1 text-xs text-zinc-200"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {ROLE_LABELS[r]}
                </option>
              ))}
            </select>
            <button
              onClick={() => update.mutate({ id: u.id, disabled: !u.disabled })}
              disabled={update.isPending}
              className="rounded-md border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50"
            >
              {u.disabled ? 'Habilitar' : 'Deshabilitar'}
            </button>
            <button
              onClick={onDelete}
              disabled={del.isPending}
              className="rounded-md border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 hover:border-[#d03b3b]/60 hover:bg-[#d03b3b]/10 disabled:opacity-50"
            >
              Eliminar
            </button>
          </>
        )}
      </div>
      {error && <p className="mt-2 text-xs text-red-400">{error.message}</p>}
    </li>
  )
}

function InviteForm() {
  const create = useCreateInvite()
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('developer')
  const [copied, setCopied] = useState(false)
  const link = create.data ? `${window.location.origin}${create.data.path}` : ''

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    setCopied(false)
    create.mutate({ email, role }, { onSuccess: () => setEmail('') })
  }

  async function copy() {
    await navigator.clipboard.writeText(link)
    setCopied(true)
  }

  return (
    <div className="mt-4">
      <form onSubmit={onSubmit} className="grid grid-cols-[1fr_12rem_auto] items-end gap-3">
        <Field label="Invitar por email">
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="persona@empresa.com"
            className={inputClass}
          />
        </Field>
        <Field label="Rol">
          <select value={role} onChange={(e) => setRole(e.target.value as Role)} className={inputClass}>
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {ROLE_LABELS[r]}
              </option>
            ))}
          </select>
        </Field>
        <button type="submit" disabled={create.isPending} className={`${primary} h-[38px]`}>
          {create.isPending ? 'Creando…' : 'Crear invitación'}
        </button>
      </form>
      {create.isError && <ErrorBox>{create.error.message}</ErrorBox>}
      {create.data && (
        <div className="mt-3 rounded-md border border-[#0ca30c]/30 bg-[#0ca30c]/5 p-3 text-xs text-zinc-300">
          <p>
            <span className="mr-1.5 text-[#0ca30c]">✓</span>
            Envía este enlace a <span className="text-zinc-100">{create.data.email}</span>. Vale 7 días y solo se muestra ahora.
          </p>
          <div className="mt-2 flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-2.5 py-1.5">
            <span className="min-w-0 flex-1 truncate font-mono text-zinc-200">{link}</span>
            <button type="button" onClick={copy} className="shrink-0 text-zinc-400 hover:text-zinc-200">
              {copied ? '✓ Copiado' : 'Copiar'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function PendingInvites() {
  const { data } = useInvites()
  const revoke = useRevokeInvite()
  if (!data?.length) return null
  return (
    <div className="mt-4">
      <h3 className="text-xs text-zinc-400">Invitaciones pendientes</h3>
      <ul className="mt-2 space-y-1">
        {data.map((inv: Invite) => (
          <li key={inv.id} className="flex items-center justify-between gap-3 rounded-md bg-zinc-900/60 px-3 py-2 text-xs">
            <span className="text-zinc-300">
              {inv.email} · {ROLE_LABELS[inv.role]}
              <span className="text-zinc-500">
                {' '}
                · invitada por {inv.createdBy || 'alguien'} · vence {formatRelative(inv.expiresAt)}
              </span>
            </span>
            <button
              onClick={() => revoke.mutate(inv.id)}
              disabled={revoke.isPending}
              className="text-zinc-400 hover:text-zinc-200 disabled:opacity-50"
            >
              Revocar
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
