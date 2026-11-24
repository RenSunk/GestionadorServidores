import { type FormEvent, useState } from 'react'
import { type AlertSettings, useAlertSettings, useSaveAlertSettings, useTestAlertWebhook } from '../lib/alerts.ts'
import { inputClass } from '../lib/ui.ts'
import { ErrorBox } from './Form.tsx'

const primary = 'rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-white disabled:opacity-50'

export function AlertSettingsSection() {
  const { data, error } = useAlertSettings()
  return (
    <section id="alertas" className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
      <h2 className="text-sm font-medium">Alertas</h2>
      <p className="mt-1 max-w-2xl text-xs text-zinc-500">
        Se revisan con cada sondeo del monitor (cada 15 s) y aparecen en la sección Alertas. Si configuras un webhook, también se avisa
        al abrirse y al resolverse.
      </p>
      {error && <ErrorBox>{error.message}</ErrorBox>}
      {data && <Form key={JSON.stringify(data)} initial={data} />}
    </section>
  )
}

function Form({ initial }: { initial: AlertSettings }) {
  const save = useSaveAlertSettings()
  const test = useTestAlertWebhook()
  const [v, setV] = useState(initial)
  const set = <K extends keyof AlertSettings>(k: K, val: AlertSettings[K]) => setV({ ...v, [k]: val })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    save.mutate(v)
  }

  const num = (k: 'cpuPercent' | 'memPercent' | 'diskPercent' | 'sustainMinutes', label: string, suffix: string, max: number) => (
    <label className="block">
      <span className="text-xs text-zinc-400">{label}</span>
      <div className="mt-1 flex items-center gap-1.5">
        <input
          type="number"
          min={k === 'sustainMinutes' ? 1 : 0}
          max={max}
          value={v[k]}
          disabled={!v.enabled}
          onChange={(e) => set(k, Number(e.target.value))}
          className={`${inputClass} mt-0 w-20 tabular-nums`}
        />
        <span className="text-xs text-zinc-500">{suffix}</span>
      </div>
    </label>
  )

  const check = (k: 'serverDown' | 'appDown' | 'deployFailed', label: string, hint: string) => (
    <label className="flex cursor-pointer items-start gap-2 text-sm">
      <input type="checkbox" checked={v[k]} disabled={!v.enabled} onChange={(e) => set(k, e.target.checked)} className="mt-1 accent-zinc-200" />
      <span>
        <span className="text-zinc-200">{label}</span>
        <span className="block text-xs text-zinc-500">{hint}</span>
      </span>
    </label>
  )

  return (
    <form onSubmit={onSubmit} className="mt-4 space-y-5">
      <label className="flex cursor-pointer items-center gap-2 text-sm">
        <input type="checkbox" checked={v.enabled} onChange={(e) => set('enabled', e.target.checked)} className="accent-zinc-200" />
        <span className="text-zinc-200">Alertas activadas</span>
      </label>

      <div className={v.enabled ? '' : 'opacity-50'}>
        <div className="text-xs font-medium text-zinc-300">Recursos del servidor</div>
        <p className="text-[11px] text-zinc-500">0 desactiva esa alerta. CPU y RAM se miden como promedio sostenido; el disco, al momento.</p>
        <div className="mt-2 flex flex-wrap gap-4">
          {num('cpuPercent', 'CPU', '%', 100)}
          {num('memPercent', 'RAM', '%', 100)}
          {num('sustainMinutes', 'Durante', 'min', 60)}
          {num('diskPercent', 'Disco', '%', 100)}
        </div>
      </div>

      <div className={`grid gap-3 sm:grid-cols-3 ${v.enabled ? '' : 'opacity-50'}`}>
        {check('serverDown', 'Servidor caído', 'Tras 2 sondeos fallidos seguidos (~30 s).')}
        {check('appDown', 'App detenida', 'El contenedor de un proyecto desplegado se detuvo o se reinicia en bucle.')}
        {check('deployFailed', 'Deploy fallido', 'Manual, por webhook o por revisión periódica.')}
      </div>

      <div>
        <label className="block">
          <span className="text-xs font-medium text-zinc-300">Webhook de aviso (opcional)</span>
          <span className="block text-[11px] text-zinc-500">
            Slack (hooks.slack.com) y Discord (discord.com/api/webhooks) reciben un mensaje con su formato; cualquier otra URL recibe JSON con
            event, text y alert. Se guarda cifrada.
          </span>
          <input
            type="url"
            value={v.webhookUrl}
            onChange={(e) => set('webhookUrl', e.target.value)}
            placeholder="https://hooks.slack.com/services/…"
            className={`${inputClass} font-mono text-xs`}
          />
        </label>
        <div className="mt-2 flex items-center gap-3 text-xs">
          <button
            type="button"
            onClick={() => test.mutate(v.webhookUrl)}
            disabled={!v.webhookUrl || test.isPending}
            className="rounded-md border border-zinc-700 px-3 py-1.5 text-zinc-200 hover:bg-zinc-800 disabled:opacity-50"
          >
            {test.isPending ? 'Enviando…' : 'Enviar prueba'}
          </button>
          {test.isSuccess && (
            <span className="text-zinc-300">
              <span className="mr-1 text-[#0ca30c]">✓</span>Enviado. Revisa el canal.
            </span>
          )}
          {test.isError && (
            <span className="text-zinc-300">
              <span className="mr-1 text-[#d03b3b]">✕</span>
              {test.error.message}
            </span>
          )}
        </div>
      </div>

      {save.isError && <ErrorBox>{save.error.message}</ErrorBox>}
      <div className="flex items-center gap-3">
        <button type="submit" disabled={save.isPending} className={primary}>
          {save.isPending ? 'Guardando…' : 'Guardar alertas'}
        </button>
        {save.isSuccess && <span className="text-xs text-zinc-400">Guardado.</span>}
      </div>
    </form>
  )
}
