import { type FormEvent, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { CopyButton } from '../components/CopyButton.tsx'
import { type AuthMethod, useCreateServer, useInstanceKey } from '../lib/servers.ts'

const methods: { value: AuthMethod; label: string; hint: string }[] = [
  { value: 'instance_key', label: 'Llave de Gestionador', hint: 'Recomendado. Copias una llave pública al servidor.' },
  { value: 'private_key', label: 'Mi llave privada', hint: 'Pegas una llave privada existente (sin passphrase).' },
  { value: 'password', label: 'Contraseña', hint: 'Menos seguro. Útil para pruebas.' },
]

const inputClass =
  'mt-1 w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none'

export function NewServerPage() {
  const navigate = useNavigate()
  const create = useCreateServer()
  const [form, setForm] = useState({
    name: '',
    host: '',
    port: '22',
    username: 'root',
    authMethod: 'instance_key' as AuthMethod,
    privateKey: '',
    password: '',
  })

  const set = (field: keyof typeof form) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    create.mutate(
      {
        name: form.name,
        host: form.host,
        port: Number(form.port) || 22,
        username: form.username,
        authMethod: form.authMethod,
        privateKey: form.authMethod === 'private_key' ? form.privateKey : undefined,
        password: form.authMethod === 'password' ? form.password : undefined,
      },
      { onSuccess: (server) => navigate(`/servers/${server.id}`) },
    )
  }

  return (
    <div className="max-w-2xl">
      <Link to="/servers" className="text-sm text-zinc-500 hover:text-zinc-300">
        ← Servidores
      </Link>
      <h1 className="mt-2 text-2xl font-semibold">Agregar servidor</h1>
      <p className="mt-1 text-sm text-zinc-400">
        Gestionador se conecta por SSH para verificar el servidor antes de guardarlo. Solo Linux.
      </p>

      <form onSubmit={onSubmit} className="mt-8 space-y-6">
        <label className="block">
          <span className="text-sm text-zinc-300">Nombre</span>
          <input required value={form.name} onChange={set('name')} placeholder="produccion-1" className={inputClass} />
        </label>

        <div className="grid grid-cols-[1fr_6rem] gap-3">
          <label className="block">
            <span className="text-sm text-zinc-300">Host o IP</span>
            <input required value={form.host} onChange={set('host')} placeholder="203.0.113.10" className={inputClass} />
          </label>
          <label className="block">
            <span className="text-sm text-zinc-300">Puerto</span>
            <input type="number" min={1} max={65535} value={form.port} onChange={set('port')} className={inputClass} />
          </label>
        </div>

        <label className="block">
          <span className="text-sm text-zinc-300">Usuario SSH</span>
          <input required value={form.username} onChange={set('username')} className={inputClass} />
        </label>

        <fieldset className="min-w-0">
          <legend className="text-sm text-zinc-300">Autenticación</legend>
          <div className="mt-2 grid gap-2 sm:grid-cols-3">
            {methods.map((m) => (
              <label
                key={m.value}
                className={`cursor-pointer rounded-md border p-3 text-sm ${
                  form.authMethod === m.value ? 'border-zinc-400 bg-zinc-900' : 'border-zinc-800 hover:border-zinc-700'
                }`}
              >
                <input
                  type="radio"
                  name="authMethod"
                  value={m.value}
                  checked={form.authMethod === m.value}
                  onChange={set('authMethod')}
                  className="sr-only"
                />
                <div className="font-medium">{m.label}</div>
                <div className="mt-1 text-xs text-zinc-500">{m.hint}</div>
              </label>
            ))}
          </div>

          <div className="mt-4">
            {form.authMethod === 'instance_key' && <InstanceKeyInstructions username={form.username} />}
            {form.authMethod === 'private_key' && (
              <textarea
                required
                value={form.privateKey}
                onChange={set('privateKey')}
                rows={8}
                spellCheck={false}
                placeholder={'-----BEGIN OPENSSH PRIVATE KEY-----\n...'}
                className={`${inputClass} font-mono text-xs`}
              />
            )}
            {form.authMethod === 'password' && (
              <input
                required
                type="password"
                value={form.password}
                onChange={set('password')}
                autoComplete="off"
                placeholder="Contraseña del usuario"
                className={inputClass}
              />
            )}
          </div>
          {form.authMethod !== 'instance_key' && (
            <p className="mt-2 text-xs text-zinc-500">Se guarda cifrada (AES-256) en tu instancia y nunca se muestra de nuevo.</p>
          )}
        </fieldset>

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
            {create.isPending ? 'Conectando…' : 'Conectar y guardar'}
          </button>
          <Link to="/servers" className="text-sm text-zinc-400 hover:text-zinc-200">
            Cancelar
          </Link>
        </div>
      </form>
    </div>
  )
}

function InstanceKeyInstructions({ username }: { username: string }) {
  const { data, isError } = useInstanceKey()
  if (isError) return <p className="text-sm text-red-400">No se pudo obtener la llave pública.</p>
  if (!data) return <p className="text-sm text-zinc-500">Cargando llave…</p>

  const command = `mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '${data.publicKey}' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys`

  return (
    <div className="rounded-md border border-zinc-800 bg-zinc-900/50 p-4 text-sm">
      <p className="text-zinc-300">
        Ejecuta esto en el servidor, con el usuario <span className="font-mono text-zinc-100">{username || '…'}</span>:
      </p>
      <div className="mt-3 flex items-start gap-2">
        <code className="block min-w-0 flex-1 whitespace-pre-wrap break-all rounded bg-zinc-950 p-2 font-mono text-xs text-zinc-300">
          {command}
        </code>
        <CopyButton text={command} />
      </div>
      <p className="mt-3 text-xs text-zinc-500">Huella de la llave: {data.fingerprint}</p>
    </div>
  )
}
