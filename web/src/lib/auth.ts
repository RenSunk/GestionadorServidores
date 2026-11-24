import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api.ts'

export type Role = 'admin' | 'developer' | 'viewer'

export const ROLE_LABELS: Record<Role, string> = {
  admin: 'Administrador',
  developer: 'Desarrollador',
  viewer: 'Solo lectura',
}

export const ROLE_HINTS: Record<Role, string> = {
  admin: 'Todo, incluidos servidores y usuarios.',
  developer: 'Crea y despliega proyectos; edita variables, dominios y configuración.',
  viewer: 'Ve el estado, los deploys y los logs. No ve secretos ni puede cambiar nada.',
}

const RANK: Record<Role, number> = { viewer: 1, developer: 2, admin: 3 }

export type User = {
  id: string
  email: string
  name: string
  role: Role
  disabled: boolean
  createdAt: string
  lastLoginAt: string | null
}

export type Invite = {
  id: string
  email: string
  role: Role
  createdBy: string
  createdAt: string
  expiresAt: string
}

export type AuthStatus = { setupRequired: boolean; needsCode: boolean }

export function useAuthStatus() {
  return useQuery({
    queryKey: ['auth', 'status'],
    queryFn: () => api<AuthStatus>('/auth/status'),
  })
}

export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => api<User>('/auth/me'),
    retry: false,
    staleTime: 60_000,
  })
}

/** can('developer') dice si el usuario actual tiene al menos ese rol. */
export function useCan() {
  const { data: me } = useMe()
  return (min: Role) => !!me && RANK[me.role] >= RANK[min]
}

function useSession<T>(fn: (v: T) => Promise<User>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: (user) => {
      qc.clear() // datos de la sesión anterior fuera
      qc.setQueryData(['me'], user)
    },
  })
}

export const useLogin = () =>
  useSession((v: { email: string; password: string }) => api<User>('/auth/login', { method: 'POST', body: JSON.stringify(v) }))

export const useSetup = () =>
  useSession((v: { name: string; email: string; password: string; code: string }) =>
    api<User>('/auth/setup', { method: 'POST', body: JSON.stringify(v) }),
  )

export const useAcceptInvite = (token: string) =>
  useSession((v: { name: string; password: string }) =>
    api<User>(`/invites/accept/${token}`, {
      method: 'POST',
      body: JSON.stringify(v),
    }),
  )

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/auth/logout', { method: 'POST' }),
    onSuccess: () => {
      qc.clear()
      window.location.assign('/login')
    },
  })
}

export function useInviteInfo(token: string, enabled = true) {
  return useQuery({
    enabled,
    queryKey: ['invite', token],
    queryFn: () => api<{ email: string; role: Role }>(`/invites/accept/${token}`),
    retry: false,
  })
}

export function useUpdateMe() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api<User>('/auth/me', { method: 'PUT', body: JSON.stringify({ name }) }),
    onSuccess: (u) => qc.setQueryData(['me'], u),
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (v: { current: string; new: string }) => api<void>('/auth/password', { method: 'POST', body: JSON.stringify(v) }),
  })
}

// ---------- Administración ----------

export function useUsers() {
  return useQuery({
    queryKey: ['users'],
    queryFn: () => api<User[]>('/users'),
  })
}

export function useUpdateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...patch }: { id: string; role?: Role; disabled?: boolean }) =>
      api<User>(`/users/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(patch),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
}

export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/users/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
}

export function useInvites() {
  return useQuery({
    queryKey: ['invites'],
    queryFn: () => api<Invite[]>('/invites'),
  })
}

export function useCreateInvite() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { email: string; role: Role }) =>
      api<Invite & { path: string }>('/invites', {
        method: 'POST',
        body: JSON.stringify(v),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invites'] })
    },
  })
}

export function useRevokeInvite() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/invites/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invites'] })
    },
  })
}
