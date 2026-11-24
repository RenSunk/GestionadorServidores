import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api.ts'

export type AuthMethod = 'instance_key' | 'private_key' | 'password'
export type ServerStatus = 'pending' | 'online' | 'offline'
export type DockerStatus = '' | 'running' | 'not_installed' | 'no_permission' | 'not_running'

export type Usage = { total: number; used: number }

export type Metrics = {
  collectedAt: string
  cpuPercent: number
  load: [number, number, number]
  memory: Usage
  swap: Usage
  disk: Usage
  uptimeSeconds: number
}

export type Server = {
  id: string
  name: string
  host: string
  port: number
  username: string
  authMethod: AuthMethod
  hostKeyFingerprint: string
  status: ServerStatus
  lastError: string
  lastSeenAt: string | null
  createdAt: string
  system: {
    os: string
    kernel: string
    arch: string
    hostname: string
    cpuCores: number
    dockerStatus: DockerStatus
    dockerVersion: string
  }
  metrics: Metrics | null
}

export type HistoryPoint = { t: string; cpuPercent: number; memoryPercent: number }
export type ServerDetail = Server & { history: HistoryPoint[] }

export type CreateServerInput = {
  name: string
  host: string
  port: number
  username: string
  authMethod: AuthMethod
  privateKey?: string
  password?: string
}

// El backend toma una muestra cada 15 s; refrescar cada 5 s mantiene la UI al día.
const REFRESH_MS = 5_000

export function useServers() {
  return useQuery({
    queryKey: ['servers'],
    queryFn: () => api<Server[]>('/servers'),
    refetchInterval: REFRESH_MS,
  })
}

export function useServer(id: string) {
  return useQuery({
    queryKey: ['servers', id],
    queryFn: () => api<ServerDetail>(`/servers/${id}`),
    refetchInterval: REFRESH_MS,
  })
}

export function useInstanceKey() {
  return useQuery({
    queryKey: ['ssh-key'],
    queryFn: () => api<{ publicKey: string; fingerprint: string }>('/ssh-key'),
    staleTime: Infinity,
  })
}

export function useCreateServer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateServerInput) =>
      api<ServerDetail>('/servers', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: (server) => {
      qc.setQueryData(['servers', server.id], server)
      qc.invalidateQueries({ queryKey: ['servers'], exact: true })
    },
  })
}

export function useCheckServer(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<ServerDetail>(`/servers/${id}/check`, { method: 'POST' }),
    onSuccess: (server) => {
      qc.setQueryData(['servers', id], server)
      qc.invalidateQueries({ queryKey: ['servers'], exact: true })
    },
  })
}

export function useDeleteServer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/servers/${id}`, { method: 'DELETE' }),
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: ['servers', id] })
      qc.invalidateQueries({ queryKey: ['servers'], exact: true })
    },
  })
}

export function usagePercent(u: Usage): number {
  return u.total > 0 ? (u.used / u.total) * 100 : 0
}
