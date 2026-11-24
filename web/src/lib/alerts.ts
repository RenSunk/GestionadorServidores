import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api.ts'

export type AlertKind = 'server_down' | 'cpu_high' | 'mem_high' | 'disk_full' | 'app_down' | 'deploy_failed'

export type Alert = {
  id: string
  kind: AlertKind
  targetType: 'server' | 'project'
  targetId: string
  targetName: string
  message: string
  startedAt: string
  resolvedAt: string | null
}

export type AlertSettings = {
  enabled: boolean
  cpuPercent: number
  memPercent: number
  diskPercent: number
  sustainMinutes: number
  serverDown: boolean
  appDown: boolean
  deployFailed: boolean
  webhookUrl: string
}

export const KIND_LABELS: Record<AlertKind, string> = {
  server_down: 'Servidor caído',
  cpu_high: 'CPU alta',
  mem_high: 'RAM alta',
  disk_full: 'Disco lleno',
  app_down: 'App detenida',
  deploy_failed: 'Deploy fallido',
}

export function useAlerts(state?: 'open') {
  return useQuery({
    queryKey: ['alerts', state ?? 'all'],
    queryFn: () => api<Alert[]>(`/alerts${state ? `?state=${state}` : ''}`),
    refetchInterval: 15_000,
  })
}

export function useAlertSettings() {
  return useQuery({ queryKey: ['alert-settings'], queryFn: () => api<AlertSettings>('/alert-settings') })
}

export function useSaveAlertSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (s: AlertSettings) => api<AlertSettings>('/alert-settings', { method: 'PUT', body: JSON.stringify(s) }),
    onSuccess: (s) => qc.setQueryData(['alert-settings'], s),
  })
}

export function useTestAlertWebhook() {
  return useMutation({
    mutationFn: (webhookUrl: string) => api<void>('/alert-settings/test', { method: 'POST', body: JSON.stringify({ webhookUrl }) }),
  })
}
