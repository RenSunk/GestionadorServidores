import { useCan } from '../lib/auth.ts'
import { type FormEvent, useState } from 'react'
import { Link } from 'react-router'
import {
  type Domain,
  type DomainCheck,
  type ProjectDetail,
  type TLSMode,
  useAddDomain,
  useCheckDomain,
  useDomains,
  useProxyInfo,
  useRemoveDomain,
} from '../lib/projects.ts'
import { inputClass } from '../lib/ui.ts'
import { ErrorBox, Field } from './Form.tsx'

export function DomainsPanel({ project: p }: { project: ProjectDetail }) {
  const { data: domains, isPending, error } = useDomains(p.id)
  const proxy = useProxyInfo(p.serverId)
  const neverDeployed = !p.deployments.some((d) => d.status === 'succeeded')
  const can = useCan()
  const httpsSuffix = proxy.data && proxy.data.httpsPort !== 443 ? `:${proxy.data.httpsPort}` : ''

  return (
    <div className="space-y-4">
      {can('developer') && <AddDomainForm project={p} />}

      {p.hostPort != null && (domains?.length ?? 0) > 0 && (
        <p className="rounded-md border border-[#fab219]/30 bg-[#fab219]/5 p-3 text-sm text-zinc-300">
          <span className="mr-1.5 text-[#fab219]">▲</span>
          El proyecto también publica el puerto {p.hostPort}. Mientras lo tenga, cada deploy tiene un corte breve. Si solo vas a
          entrar por el dominio, quítalo en Configuración para desplegar sin corte.
        </p>
      )}
      {neverDeployed && (domains?.length ?? 0) > 0 && (
        <p className="text-sm text-zinc-400">
          <span className="mr-1.5 text-zinc-500">○</span>El dominio empezará a responder después del primer deploy.
        </p>
      )}

      <section className="overflow-hidden rounded-lg border border-zinc-800">
        {isPending && <p className="p-4 text-sm text-zinc-500">Cargando…</p>}
        {error && <p className="p-4 text-sm text-red-400">{error.message}</p>}
        {domains?.length === 0 && (
          <p className="p-4 text-sm text-zinc-500">
            Todavía no hay dominios. Al agregar uno, Gestionador configura Caddy en {p.serverName} y obtiene el certificado HTTPS automáticamente.
          </p>
        )}
        <ul className="divide-y divide-zinc-800">
          {domains?.map((d) => (
            <DomainRow key={d.id} domain={d} project={p} httpsSuffix={httpsSuffix} />
          ))}
        </ul>
      </section>

      <p className="text-xs text-zinc-500">
        Proxy en {p.serverName}:{' '}
        {proxy.data ? (
          <>
            Caddy {proxy.data.state === 'running' ? 'en ejecución' : proxy.data.state === 'missing' ? 'aún no instalado' : proxy.data.state} · puertos{' '}
            {proxy.data.httpPort}/{proxy.data.httpsPort}
          </>
        ) : (
          '…'
        )}{' '}
        ·{' '}
        <Link to={`/servers/${p.serverId}`} className="underline hover:text-zinc-300">
          configurar
        </Link>
      </p>
    </div>
  )
}

function AddDomainForm({ project: p }: { project: ProjectDetail }) {
  const add = useAddDomain(p.id)
  const [hostname, setHostname] = useState('')
  const [tlsMode, setTlsMode] = useState<TLSMode>('auto')

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    add.mutate({ hostname, tlsMode }, { onSuccess: () => setHostname('') })
  }

  return (
    <form onSubmit={onSubmit} className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <h2 className="text-sm font-medium">Agregar dominio</h2>
      <div className="mt-3 grid grid-cols-[1fr_16rem_auto] items-end gap-3">
        <Field label="Dominio">
          <input
            required
            value={hostname}
            onChange={(e) => setHostname(e.target.value)}
            placeholder="app.midominio.com"
            className={`${inputClass} font-mono text-xs`}
          />
        </Field>
        <Field label="Certificado HTTPS">
          <select value={tlsMode} onChange={(e) => setTlsMode(e.target.value as TLSMode)} className={inputClass}>
            <option value="auto">Automático (Let's Encrypt)</option>
            <option value="internal">Interno (pruebas o red local)</option>
          </select>
        </Field>
        <button
          type="submit"
          disabled={add.isPending || !hostname.trim()}
          className="h-[38px] rounded-md bg-zinc-100 px-4 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50"
        >
          {add.isPending ? 'Configurando…' : 'Agregar'}
        </button>
      </div>
      <p className="mt-2 text-xs text-zinc-500">
        {tlsMode === 'auto'
          ? `Antes, crea un registro DNS A del dominio apuntando a la IP de ${p.serverName}, y deja abiertos los puertos 80 y 443.`
          : 'Caddy firma el certificado con su propia autoridad: funciona sin DNS público, pero los navegadores lo marcan como no confiable.'}
      </p>
      {add.isError && <ErrorBox>{add.error.message}</ErrorBox>}
    </form>
  )
}

function DomainRow({ domain: d, project: p, httpsSuffix }: { domain: Domain; project: ProjectDetail; httpsSuffix: string }) {
  const check = useCheckDomain()
  const remove = useRemoveDomain(p.id)
  const can = useCan()

  function onRemove() {
    if (!confirm(`¿Quitar ${d.hostname}? Dejará de responder de inmediato.`)) return
    remove.mutate(d.id, { onSuccess: ({ warning }) => warning && alert(warning) })
  }

  return (
    <li className="px-4 py-3 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <a
            href={`https://${d.hostname}${httpsSuffix}`}
            target="_blank"
            rel="noreferrer"
            className="font-mono text-xs text-zinc-100 hover:underline"
          >
            {d.hostname} ↗
          </a>
          <span className="ml-2 rounded border border-zinc-700 px-1.5 py-0.5 text-[11px] text-zinc-400">
            {d.tlsMode === 'auto' ? "Let's Encrypt" : 'Certificado interno'}
          </span>
        </div>
        {can('developer') && (
        <div className="flex gap-2">
          <button
            onClick={() => check.mutate(d.id)}
            disabled={check.isPending}
            className="rounded-md border border-zinc-700 px-2.5 py-1 text-xs text-zinc-200 hover:bg-zinc-800 disabled:opacity-50"
          >
            {check.isPending ? 'Verificando…' : 'Verificar'}
          </button>
          <button
            onClick={onRemove}
            disabled={remove.isPending}
            className="rounded-md border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 hover:border-[#d03b3b]/60 hover:bg-[#d03b3b]/10 disabled:opacity-50"
          >
            Quitar
          </button>
        </div>
        )}
      </div>
      {check.data && <CheckResult result={check.data} />}
      {check.isError && <p className="mt-2 text-xs text-red-400">{check.error.message}</p>}
      {remove.isError && <p className="mt-2 text-xs text-red-400">{remove.error.message}</p>}
    </li>
  )
}

function CheckResult({ result: r }: { result: DomainCheck }) {
  return (
    <div className="mt-3 space-y-1.5 rounded-md bg-zinc-900/60 p-3 text-xs">
      <CheckLine ok={r.dns.ok} warn={!r.dns.ok && r.dns.records.length > 0} label="DNS" text={r.dns.message} />
      <CheckLine
        ok={r.https.ok}
        label="HTTPS"
        text={`${r.https.message}${r.https.status ? ` (HTTP ${r.https.status}${r.https.issuer ? ` · emitido por ${r.https.issuer}` : ''})` : ''}`}
      />
    </div>
  )
}

function CheckLine({ ok, warn, label, text }: { ok: boolean; warn?: boolean; label: string; text: string }) {
  const icon = ok ? <span className="text-[#0ca30c]">●</span> : warn ? <span className="text-[#fab219]">▲</span> : <span className="text-[#d03b3b]">✕</span>
  return (
    <div className="flex gap-2">
      <span className="w-3 shrink-0">{icon}</span>
      <span className="w-12 shrink-0 text-zinc-400">{label}</span>
      <span className="text-zinc-300">{text}</span>
    </div>
  )
}
