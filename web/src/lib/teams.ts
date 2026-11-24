import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api.ts'
import { useCan } from './auth.ts'

export type Team = {
  id: string
  name: string
  createdAt: string
  userIds: string[]
  projectIds: string[]
  serverIds: string[]
}

export type TeamInput = Omit<Team, 'id' | 'createdAt'>

/** Equipos (solo administradores: para el resto no se consulta). */
export function useTeams() {
  const can = useCan()
  return useQuery({ queryKey: ['teams'], queryFn: () => api<Team[]>('/teams'), enabled: can('admin') })
}

export function useSaveTeam() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...input }: TeamInput & { id?: string }) =>
      id
        ? api<Team>(`/teams/${id}`, { method: 'PUT', body: JSON.stringify(input) })
        : api<Team>('/teams', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['teams'] })
    },
  })
}

export function useDeleteTeam() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/teams/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['teams'] })
    },
  })
}

/** Nombres de los equipos que restringen un recurso ([] = visible para todos). */
export function useAccess(kind: 'project' | 'server', id: string) {
  const { data } = useTeams()
  if (!data) return null
  return data.filter((t) => (kind === 'project' ? t.projectIds : t.serverIds).includes(id)).map((t) => t.name)
}
