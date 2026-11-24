import { NavLink, Outlet } from 'react-router'
import { useAlerts } from '../lib/alerts.ts'
import { ROLE_LABELS, useLogout, useMe } from '../lib/auth.ts'

const nav = [
  { to: '/', label: 'Dashboard' },
  { to: '/servers', label: 'Servidores' },
  { to: '/projects', label: 'Proyectos' },
  { to: '/alerts', label: 'Alertas' },
  { to: '/settings', label: 'Configuración' },
]

export function Layout() {
  const { data: openAlerts } = useAlerts('open')
  return (
    <div className="flex h-full bg-zinc-950 text-zinc-100">
      <aside className="flex w-60 shrink-0 flex-col border-r border-zinc-800 p-4">
        <div className="mb-8 px-2 text-lg font-semibold tracking-tight">Gestionador</div>
        <nav className="flex flex-col gap-1">
          {nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              className={({ isActive }) =>
                `rounded-md px-2 py-1.5 text-sm ${
                  isActive ? 'bg-zinc-800 text-white' : 'text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200'
                }`
              }
            >
              <span className="flex items-center justify-between">
                {item.label}
                {item.to === '/alerts' && !!openAlerts?.length && (
                  <span
                    className="rounded-full bg-[#d03b3b] px-1.5 text-[11px] font-medium tabular-nums text-white"
                    aria-label={`${openAlerts.length} alertas activas`}
                  >
                    {openAlerts.length}
                  </span>
                )}
              </span>
            </NavLink>
          ))}
        </nav>
        <UserMenu />
      </aside>
      <main className="flex-1 overflow-y-auto p-8">
        <Outlet />
      </main>
    </div>
  )
}

function UserMenu() {
  const { data: me } = useMe()
  const logout = useLogout()
  if (!me) return null
  return (
    <div className="mt-auto border-t border-zinc-800 px-2 pt-4">
      <div className="truncate text-sm text-zinc-200">{me.name}</div>
      <div className="truncate text-xs text-zinc-500">
        {me.email} · {ROLE_LABELS[me.role]}
      </div>
      <button
        onClick={() => logout.mutate()}
        disabled={logout.isPending}
        className="mt-2 text-xs text-zinc-400 hover:text-zinc-200 disabled:opacity-50"
      >
        Cerrar sesión
      </button>
    </div>
  )
}
