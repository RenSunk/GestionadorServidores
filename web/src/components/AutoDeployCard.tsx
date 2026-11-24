import { type ReactNode, useState } from 'react'
import { formatRelative } from '../lib/format.ts'
import { type AutoDeploy, type ProjectDetail, useAutoDeploy, useRotateSecret, useSetAutoDeploy } from '../lib/projects.ts'
import { inputClass } from '../lib/ui.ts'
import { ErrorBox } from './Form.tsx'

const POLL_OPTIONS = [
  { value: 0, label: 'Desactivada' },
  { value: 1, label: 'Cada minuto' },
  { value: 5, label: 'Cada 5 minutos' },
  { value: 15, label: 'Cada 15 minutos' },
  { value: 60, label: 'Cada hora' },
]

type Provider = 'github' | 'gitlab' | 'gitea' | 'bitbucket'

// Direcciones a las que GitHub & co. no pueden llegar desde internet.
function isPrivateHost(host: string) {
  return (
    host === 'localhost' ||
    host === '[::1]' ||
    host.endsWith('.local') ||
    /^(127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(host)
  )
}

export function AutoDeployCard({ project: p }: { project: ProjectDetail }) {
  const { data, isPending, error } = useAutoDeploy(p.id)
  const save = useSetAutoDeploy(p.id)

  return (
    <section className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-sm font-medium">Deploy automático</h2>
          <p className="mt-1 max-w-xl text-xs text-zinc-500">
            Despliega la rama <span className="font-mono text-zinc-400">{p.branch}</span> cada vez que haces push. Si llega un push
            durante un deploy, queda en cola y se despliega al terminar.
          </p>
        </div>
        {data && (
          <label className="flex shrink-0 cursor-pointer items-center gap-2 text-sm text-zinc-300">
            <input
              type="checkbox"
              role="switch"
              checked={data.enabled}
              disabled={save.isPending}
              onChange={(e) => save.mutate({ enabled: e.target.checked, pollMinutes: data.pollMinutes })}
              className="peer sr-only"
            />
            <span className="relative h-5 w-9 rounded-full bg-zinc-700 transition peer-checked:bg-[#0ca30c] peer-focus-visible:ring-2 peer-focus-visible:ring-zinc-400 after:absolute after:top-0.5 after:left-0.5 after:h-4 after:w-4 after:rounded-full after:bg-white after:transition peer-checked:after:translate-x-4" />
            {data.enabled ? 'Activado' : 'Desactivado'}
          </label>
        )}
      </div>

      {isPending && <p className="mt-3 text-sm text-zinc-500">Cargando…</p>}
      {error && <ErrorBox>{error.message}</ErrorBox>}
      {save.isError && <ErrorBox>{save.error.message}</ErrorBox>}
      {data?.enabled && <AutoDeployDetails project={p} info={data} save={save} />}
    </section>
  )
}

function AutoDeployDetails({ project: p, info, save }: { project: ProjectDetail; info: AutoDeploy; save: ReturnType<typeof useSetAutoDeploy> }) {
  const rotate = useRotateSecret(p.id)
  const [reveal, setReveal] = useState(false)
  const [provider, setProvider] = useState<Provider>(guessProvider(p.repoUrl))
  const hookUrl = `${window.location.origin}${info.hookPath}`
  const unreachable = isPrivateHost(window.location.hostname)

  function onRotate() {
    if (!confirm('¿Generar un secreto nuevo? El webhook dejará de funcionar hasta que pegues el secreto nuevo en tu proveedor de Git.')) return
    rotate.mutate()
  }

  return (
    <div className="mt-4 space-y-5">
      {/* Webhook */}
      <div>
        <h3 className="text-sm text-zinc-200">Webhook <span className="text-zinc-500">· instantáneo</span></h3>
        <div className="mt-2 grid gap-2 sm:grid-cols-[7rem_1fr]">
          <span className="pt-2 text-xs text-zinc-400">URL</span>
          <CopyField value={hookUrl} />
          <span className="pt-2 text-xs text-zinc-400">Secreto</span>
          <div className="flex gap-2">
            <CopyField value={info.secret} masked={!reveal} />
            <button type="button" onClick={() => setReveal((v) => !v)} className="shrink-0 text-xs text-zinc-400 hover:text-zinc-200">
              {reveal ? 'Ocultar' : 'Mostrar'}
            </button>
            <button
              type="button"
              onClick={onRotate}
              disabled={rotate.isPending}
              className="shrink-0 text-xs text-zinc-400 hover:text-zinc-200 disabled:opacity-50"
            >
              Regenerar
            </button>
          </div>
        </div>

        {unreachable && (
          <p className="mt-3 rounded-md border border-[#fab219]/30 bg-[#fab219]/5 p-2.5 text-xs text-zinc-300">
            <span className="mr-1.5 text-[#fab219]">▲</span>
            Estás usando Gestionador en <span className="font-mono">{window.location.host}</span>, que no es accesible desde internet:
            el proveedor de Git no podrá entregar el webhook. Usa la revisión periódica de abajo, o abre Gestionador con su dirección
            pública. Todavía no tiene usuarios ni contraseña, así que no lo expongas a internet sin protegerlo.
          </p>
        )}

        <div className="mt-3 rounded-md border border-zinc-800">
          <div className="flex border-b border-zinc-800 text-xs">
            {(['github', 'gitlab', 'gitea', 'bitbucket'] as const).map((id) => (
              <button
                key={id}
                type="button"
                onClick={() => setProvider(id)}
                className={`px-3 py-2 ${provider === id ? 'border-b-2 border-zinc-200 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'}`}
              >
                {{ github: 'GitHub', gitlab: 'GitLab', gitea: 'Gitea / Forgejo', bitbucket: 'Bitbucket' }[id]}
              </button>
            ))}
          </div>
          <ol className="list-decimal space-y-1 p-3 pl-7 text-xs text-zinc-400">{steps[provider]}</ol>
        </div>

        <StatusLine
          label="Último webhook"
          at={info.lastHookAt}
          text={info.lastHookResult}
          tone={info.lastHookResult.startsWith('Rechazado') ? 'bad' : info.lastHookResult.startsWith('Ignorado') ? 'warn' : 'ok'}
          empty="Todavía no llegó ninguno. Al guardar el webhook, tu proveedor envía una prueba."
        />
      </div>

      {/* Revisión periódica */}
      <div>
        <h3 className="text-sm text-zinc-200">
          Revisión periódica <span className="text-zinc-500">· sin webhook, funciona aunque Gestionador no sea accesible desde internet</span>
        </h3>
        <div className="mt-2 flex items-center gap-3">
          <div className="w-48 shrink-0">
          <select
            value={info.pollMinutes}
            disabled={save.isPending}
            onChange={(e) => save.mutate({ enabled: info.enabled, pollMinutes: Number(e.target.value) })}
            className={inputClass}
            aria-label="Frecuencia de revisión"
          >
            {POLL_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          </div>
          <span className="text-xs text-zinc-500">Compara el último commit de la rama con el último desplegado.</span>
        </div>
        {info.pollMinutes > 0 && (
          <StatusLine
            label="Última revisión"
            at={info.lastPollAt}
            text={info.lastPollResult}
            tone={info.lastPollFailed ? 'bad' : 'ok'}
            empty="Pendiente: la primera revisión ocurre en menos de un minuto."
          />
        )}
      </div>
      {rotate.isError && <ErrorBox>{rotate.error.message}</ErrorBox>}
    </div>
  )
}

function CopyField({ value, masked }: { value: string; masked?: boolean }) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className="flex min-w-0 flex-1 items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-2.5 py-1.5">
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-zinc-200">{masked ? '•'.repeat(24) : value}</span>
      <button type="button" onClick={copy} className="shrink-0 text-xs text-zinc-400 hover:text-zinc-200">
        {copied ? '✓ Copiado' : 'Copiar'}
      </button>
    </div>
  )
}

function StatusLine(props: { label: string; at: string | null; text: string; tone: 'ok' | 'warn' | 'bad'; empty: string }) {
  const icon = { ok: <span className="text-[#0ca30c]">●</span>, warn: <span className="text-[#fab219]">▲</span>, bad: <span className="text-[#d03b3b]">✕</span> }
  return (
    <p className="mt-3 flex gap-2 text-xs">
      <span className="w-3 shrink-0">{props.at ? icon[props.tone] : <span className="text-zinc-600">○</span>}</span>
      <span className="shrink-0 text-zinc-400">{props.label}:</span>
      <span className="text-zinc-300">
        {props.at ? (
          <>
            {props.text} <span className="text-zinc-500">· {formatRelative(props.at)}</span>
          </>
        ) : (
          <span className="text-zinc-500">{props.empty}</span>
        )}
      </span>
    </p>
  )
}

function guessProvider(repoUrl: string): Provider {
  if (repoUrl.includes('gitlab')) return 'gitlab'
  if (repoUrl.includes('bitbucket')) return 'bitbucket'
  if (repoUrl.includes('github.com')) return 'github'
  return 'gitea'
}

const b = (t: string) => <span className="text-zinc-200">{t}</span>

const steps: Record<Provider, ReactNode> = {
  github: (
    <>
      <li>En el repositorio: {b('Settings → Webhooks → Add webhook')}.</li>
      <li>{b('Payload URL')}: la URL de arriba. {b('Content type')}: application/json.</li>
      <li>{b('Secret')}: el secreto de arriba. Evento: {b('Just the push event')}.</li>
    </>
  ),
  gitlab: (
    <>
      <li>En el proyecto: {b('Settings → Webhooks → Add new webhook')}.</li>
      <li>{b('URL')}: la URL de arriba. {b('Secret token')}: el secreto de arriba.</li>
      <li>Trigger: {b('Push events')} (puedes filtrar por la rama del proyecto).</li>
    </>
  ),
  gitea: (
    <>
      <li>En el repositorio: {b('Settings → Webhooks → Add webhook → Gitea')} (o Forgejo).</li>
      <li>{b('Target URL')}: la URL de arriba. {b('POST Content Type')}: application/json.</li>
      <li>{b('Secret')}: el secreto de arriba. Trigger: {b('Push events')}.</li>
    </>
  ),
  bitbucket: (
    <>
      <li>En el repositorio: {b('Repository settings → Webhooks → Add webhook')}.</li>
      <li>{b('URL')}: la URL de arriba. {b('Secret')}: el secreto de arriba.</li>
      <li>Trigger: {b('Repository push')}.</li>
    </>
  ),
}
