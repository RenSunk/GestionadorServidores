import { useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useEffect } from 'react'
import { Navigate, useLocation } from 'react-router'
import { ApiError, UNAUTHORIZED_EVENT } from '../lib/api.ts'
import { useAuthStatus, useMe } from '../lib/auth.ts'

/** Muestra el contenido solo con sesión; si no, va a la creación de la primera cuenta o al login. */
export function AuthGate({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const location = useLocation()
  const me = useMe()
  const unauthenticated = me.error instanceof ApiError && me.error.status === 401
  const status = useAuthStatus()

  // Cualquier 401 de la API (sesión vencida o cerrada en otra pestaña) vuelve al login.
  useEffect(() => {
    const onUnauthorized = () => qc.resetQueries({ queryKey: ['me'] })
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
  }, [qc])

  if (me.isPending || (unauthenticated && status.isPending)) {
    return <div className="flex h-full items-center justify-center bg-zinc-950 text-sm text-zinc-500">Cargando…</div>
  }
  if (unauthenticated) {
    if (status.data?.setupRequired) return <Navigate to="/setup" replace />
    const next = location.pathname + location.search
    return <Navigate to={next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`} replace />
  }
  if (me.isError) {
    return <div className="flex h-full items-center justify-center bg-zinc-950 text-sm text-red-400">{me.error.message}</div>
  }
  return children
}
