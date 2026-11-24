import { type FormEvent, type ReactNode, useState } from 'react'
import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router'
import { ErrorBox, Field } from '../components/Form.tsx'
import { ROLE_LABELS, useAcceptInvite, useAuthStatus, useInviteInfo, useLogin, useSetup } from '../lib/auth.ts'
import { inputClass } from '../lib/ui.ts'

function AuthShell({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-h-full items-center justify-center bg-zinc-950 p-6 text-zinc-100">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center text-lg font-semibold tracking-tight">Gestionador</div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-6">
          <h1 className="text-xl font-semibold">{title}</h1>
          {subtitle && <p className="mt-1 text-sm text-zinc-400">{subtitle}</p>}
          <div className="mt-5">{children}</div>
        </div>
      </div>
    </div>
  )
}

function Submit({ pending, children }: { pending: boolean; children: ReactNode }) {
  return (
    <button
      type="submit"
      disabled={pending}
      className="mt-5 w-full rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50"
    >
      {children}
    </button>
  )
}

// Solo redirige a rutas internas (evita ?next=https://otro-sitio).
function safeNext(next: string | null) {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/'
}

export function LoginPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const status = useAuthStatus()
  const login = useLogin()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  if (status.data?.setupRequired) return <Navigate to="/setup" replace />

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    login.mutate(
      { email, password },
      {
        onSuccess: () => navigate(safeNext(params.get('next')), { replace: true }),
      },
    )
  }

  return (
    <AuthShell title="Iniciar sesión">
      <form onSubmit={onSubmit} className="space-y-4">
        <Field label="Email">
          <input
            type="email"
            required
            autoFocus
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className={inputClass}
          />
        </Field>
        <Field label="Contraseña">
          <input
            type="password"
            required
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className={inputClass}
          />
        </Field>
        {login.isError && <ErrorBox>{login.error.message}</ErrorBox>}
        <Submit pending={login.isPending}>{login.isPending ? 'Entrando…' : 'Entrar'}</Submit>
      </form>
    </AuthShell>
  )
}

function AccountFields(props: {
  name: string
  setName: (v: string) => void
  password: string
  setPassword: (v: string) => void
  repeat: string
  setRepeat: (v: string) => void
}) {
  return (
    <>
      <Field label="Tu nombre">
        <input required autoComplete="name" value={props.name} onChange={(e) => props.setName(e.target.value)} className={inputClass} />
      </Field>
      <Field label="Contraseña" hint="Al menos 10 caracteres.">
        <input
          type="password"
          required
          minLength={10}
          maxLength={72}
          autoComplete="new-password"
          value={props.password}
          onChange={(e) => props.setPassword(e.target.value)}
          className={inputClass}
        />
      </Field>
      <Field label="Repite la contraseña">
        <input
          type="password"
          required
          autoComplete="new-password"
          value={props.repeat}
          onChange={(e) => props.setRepeat(e.target.value)}
          className={inputClass}
        />
      </Field>
    </>
  )
}

export function SetupPage() {
  const navigate = useNavigate()
  const status = useAuthStatus()
  const setup = useSetup()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [code, setCode] = useState('')
  const mismatch = repeat !== '' && repeat !== password

  if (status.isPending) return null
  if (status.data && !status.data.setupRequired) return <Navigate to="/login" replace />

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (mismatch) return
    setup.mutate({ name, email, password, code }, { onSuccess: () => navigate('/', { replace: true }) })
  }

  return (
    <AuthShell title="Crea la cuenta de administrador" subtitle="Es la primera cuenta de esta instancia. Después podrás invitar a tu equipo.">
      <form onSubmit={onSubmit} className="space-y-4">
        <Field label="Email">
          <input type="email" required autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} className={inputClass} />
        </Field>
        <AccountFields name={name} setName={setName} password={password} setPassword={setPassword} repeat={repeat} setRepeat={setRepeat} />
        {status.data?.needsCode && (
          <Field label="Código de instalación" hint="Lo muestra Gestionador en su log al arrancar. Así nadie más puede reclamar la instancia.">
            <input required value={code} onChange={(e) => setCode(e.target.value)} className={`${inputClass} font-mono uppercase`} />
          </Field>
        )}
        {mismatch && <ErrorBox>Las contraseñas no coinciden.</ErrorBox>}
        {setup.isError && <ErrorBox>{setup.error.message}</ErrorBox>}
        <Submit pending={setup.isPending}>{setup.isPending ? 'Creando…' : 'Crear cuenta'}</Submit>
      </form>
    </AuthShell>
  )
}

export function InvitePage() {
  const { token = '' } = useParams()
  const navigate = useNavigate()
  const accept = useAcceptInvite(token)
  // Tras aceptar, la invitación ya no existe: no se vuelve a consultar.
  const info = useInviteInfo(token, !accept.isSuccess)
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const mismatch = repeat !== '' && repeat !== password

  if (info.isPending) return null
  if (info.isError) {
    return (
      <AuthShell title="Invitación no válida">
        <p className="text-sm text-zinc-400">{info.error.message}. Pide a un administrador que te envíe una nueva.</p>
      </AuthShell>
    )
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (mismatch) return
    accept.mutate({ name, password }, { onSuccess: () => navigate('/', { replace: true }) })
  }

  return (
    <AuthShell
      title="Únete a Gestionador"
      subtitle={
        <>
          Te invitaron como <span className="text-zinc-200">{ROLE_LABELS[info.data.role]}</span> con{' '}
          <span className="text-zinc-200">{info.data.email}</span>.
        </>
      }
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <AccountFields name={name} setName={setName} password={password} setPassword={setPassword} repeat={repeat} setRepeat={setRepeat} />
        {mismatch && <ErrorBox>Las contraseñas no coinciden.</ErrorBox>}
        {accept.isError && <ErrorBox>{accept.error.message}</ErrorBox>}
        <Submit pending={accept.isPending}>{accept.isPending ? 'Creando…' : 'Crear mi cuenta'}</Submit>
      </form>
    </AuthShell>
  )
}
