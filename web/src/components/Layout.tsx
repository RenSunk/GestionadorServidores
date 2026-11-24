import { NavLink, Outlet } from 'react-router'

type NavItem = { to: string; label: string; soon?: boolean }

// Las secciones marcadas como "soon" se habilitan a medida que avanzamos por fases.
const nav: NavItem[] = [
  { to: '/', label: 'Dashboard' },
  { to: '/servers', label: 'Servidores' },
  { to: '/projects', label: 'Proyectos' },
  { to: '/settings', label: 'Configuración', soon: true },
]

export function Layout() {
  return (
    <div className="flex h-full bg-zinc-950 text-zinc-100">
      <aside className="flex w-60 shrink-0 flex-col border-r border-zinc-800 p-4">
        <div className="mb-8 px-2 text-lg font-semibold tracking-tight">Gestionador</div>
        <nav className="flex flex-col gap-1">
          {nav.map((item) =>
            item.soon ? (
              <span
                key={item.to}
                className="flex items-center justify-between rounded-md px-2 py-1.5 text-sm text-zinc-600"
              >
                {item.label}
                <span className="text-[10px] uppercase tracking-wide">pronto</span>
              </span>
            ) : (
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
                {item.label}
              </NavLink>
            ),
          )}
        </nav>
      </aside>
      <main className="flex-1 overflow-y-auto p-8">
        <Outlet />
      </main>
    </div>
  )
}
