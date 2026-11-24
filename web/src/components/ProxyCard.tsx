import { type FormEvent, useState } from 'react'
import { type ProxyInfo, useProxyInfo, useSetProxyPorts } from '../lib/projects.ts'
import { inputClass } from '../lib/ui.ts'
import { ErrorBox, Field } from './Form.tsx'

const stateText: Record<string, string> = {
  running: 'En ejecución',
  missing: 'No instalado (se instala al agregar el primer dominio)',
  exited: 'Detenido',
  restarting: 'Reiniciándose',
  unknown: 'Desconocido',
}

/** Estado y puertos de Caddy en un servidor. */
export function ProxyCard({ serverId }: { serverId: string }) {
  const { data, error } = useProxyInfo(serverId)
  return (
    <section className="mt-4 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <h2 className="text-sm font-medium">Proxy y HTTPS (Caddy)</h2>
      <p className="mt-1 text-xs text-zinc-500">Recibe el tráfico de los dominios de los proyectos y obtiene sus certificados.</p>
      {error && <ErrorBox>{error.message}</ErrorBox>}
      {data && <ProxyForm key={`${data.httpPort}-${data.httpsPort}`} serverId={serverId} info={data} />}
    </section>
  )
}

function ProxyForm({ serverId, info }: { serverId: string; info: ProxyInfo }) {
  const save = useSetProxyPorts(serverId)
  const [ports, setPorts] = useState({ http: String(info.httpPort), https: String(info.httpsPort) })
  const dirty = ports.http !== String(info.httpPort) || ports.https !== String(info.httpsPort)
  const icon =
    info.state === 'running' ? <span className="text-[#0ca30c]">●</span> : info.state === 'missing' ? <span className="text-zinc-500">○</span> : <span className="text-[#d03b3b]">✕</span>

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const msg =
      info.domains > 0
        ? 'Caddy se recreará con los puertos nuevos; los dominios dejarán de responder unos segundos. ¿Continuar?'
        : '¿Guardar los puertos?'
    if (confirm(msg)) save.mutate({ httpPort: Number(ports.http), httpsPort: Number(ports.https) })
  }

  return (
    <form onSubmit={onSubmit} className="mt-3 space-y-3">
      <div className="flex flex-wrap gap-x-8 gap-y-1 text-sm">
        <span className="flex items-center gap-1.5">
          {icon} {stateText[info.state] ?? info.state}
        </span>
        <span className="text-zinc-400">{info.domains} dominio{info.domains === 1 ? '' : 's'}</span>
        {info.error && <span className="text-xs text-zinc-500">{info.error}</span>}
      </div>
      <div className="grid max-w-md grid-cols-[1fr_1fr_auto] items-end gap-3">
        <Field label="Puerto HTTP">
          <input type="number" min={1} max={65535} value={ports.http} onChange={(e) => setPorts({ ...ports, http: e.target.value })} className={inputClass} />
        </Field>
        <Field label="Puerto HTTPS">
          <input type="number" min={1} max={65535} value={ports.https} onChange={(e) => setPorts({ ...ports, https: e.target.value })} className={inputClass} />
        </Field>
        <button
          type="submit"
          disabled={!dirty || save.isPending}
          className="h-[38px] rounded-md border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800 disabled:opacity-50"
        >
          {save.isPending ? 'Guardando…' : 'Guardar'}
        </button>
      </div>
      <p className="text-xs text-zinc-500">
        Let's Encrypt necesita 80 y 443 abiertos desde internet. Cambia los puertos solo si ya están ocupados en el servidor o para pruebas
        con certificados internos.
      </p>
      {save.isError && <ErrorBox>{save.error.message}</ErrorBox>}
    </form>
  )
}
