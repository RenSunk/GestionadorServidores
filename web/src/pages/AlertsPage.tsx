import { Link } from 'react-router'
import { AlertsList } from '../components/AlertsList.tsx'
import { ErrorBox } from '../components/Form.tsx'
import { useAlerts } from '../lib/alerts.ts'
import { useCan } from '../lib/auth.ts'

export function AlertsPage() {
  const can = useCan()
  const { data, isPending, error } = useAlerts()
  const open = data?.filter((a) => !a.resolvedAt) ?? []
  const past = data?.filter((a) => a.resolvedAt) ?? []

  return (
    <div className="max-w-4xl">
      <h1 className="text-2xl font-semibold">Alertas</h1>
      <p className="mt-1 text-sm text-zinc-400">
        Servidores caídos, recursos al límite, apps detenidas y deploys fallidos.
        {can('admin') && (
          <>
            {' '}
            Los umbrales y el aviso por Slack, Discord o webhook se configuran en{' '}
            <Link to="/settings#alertas" className="underline hover:text-zinc-200">
              Configuración
            </Link>
            .
          </>
        )}
      </p>
      {isPending && <p className="mt-6 text-sm text-zinc-500">Cargando…</p>}
      {error && <ErrorBox>{error.message}</ErrorBox>}

      {data && (
        <>
          <section className="mt-6 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
            <h2 className="text-sm font-medium">Activas ({open.length})</h2>
            {open.length === 0 ? (
              <p className="mt-2 text-sm text-zinc-500">
                <span className="mr-1.5 text-[#0ca30c]">●</span>Todo en orden: no hay alertas activas.
              </p>
            ) : (
              <div className="mt-1">
                <AlertsList alerts={open} />
              </div>
            )}
          </section>
          <section className="mt-4 rounded-lg border border-zinc-800 bg-zinc-900/50 p-4">
            <h2 className="text-sm font-medium">Historial</h2>
            <p className="mt-1 text-xs text-zinc-500">Se conservan 90 días.</p>
            {past.length === 0 ? (
              <p className="mt-2 text-sm text-zinc-500">Todavía no hay alertas resueltas.</p>
            ) : (
              <div className="mt-1">
                <AlertsList alerts={past} />
              </div>
            )}
          </section>
        </>
      )}
    </div>
  )
}
