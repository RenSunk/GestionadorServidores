import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api.ts'

export type DeploymentStatus = 'queued' | 'building' | 'deploying' | 'succeeded' | 'failed' | 'canceled'

export type Deployment = {
  id: string
  projectId: string
  status: DeploymentStatus
  commitSha: string
  commitMessage: string
  image: string
  error: string
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
}

export type DeploymentDetail = Deployment & { log: string; live: boolean }

export type Project = {
  id: string
  name: string
  slug: string
  serverId: string
  serverName: string
  repoUrl: string
  branch: string
  gitUsername: string
  hasToken: boolean
  rootDir: string
  dockerfile: string
  containerPort: number
  hostPort: number | null
  url: string
  containerName: string
  createdAt: string
  lastDeployment: Deployment | null
  activeDeploymentId: string
}

export type ProjectDetail = Project & { deployments: Deployment[] }

export type ContainerStatus = {
  state: 'running' | 'exited' | 'restarting' | 'created' | 'paused' | 'dead' | 'missing' | 'unknown'
  startedAt?: string | null
  restartCount?: number
  deploymentId?: string
  cpuPercent?: number
  memoryUsage?: string
  memoryPercent?: number
  netIO?: string
  error?: string
}

export type CreateProjectInput = {
  name: string
  serverId: string
  repoUrl: string
  branch: string
  gitUsername?: string
  gitToken?: string
  rootDir?: string
  dockerfile?: string
  containerPort: number
  hostPort: number | null
}

/** "usuario/repo" a partir de la URL del repositorio. */
export function repoName(url: string) {
  return url.replace(/^https?:\/\/[^/]+\//, '').replace(/\.git$/, '')
}

export function isActive(status: DeploymentStatus) {
  return status === 'queued' || status === 'building' || status === 'deploying'
}

export function useProjects() {
  return useQuery({
    queryKey: ['projects'],
    queryFn: () => api<Project[]>('/projects'),
    refetchInterval: 5_000,
  })
}

export function useProject(id: string) {
  return useQuery({
    queryKey: ['projects', id],
    queryFn: () => api<ProjectDetail>(`/projects/${id}`),
    // Con un deploy en curso refrescamos más seguido para ver el cambio de estado.
    refetchInterval: (q) => (q.state.data?.activeDeploymentId ? 2_000 : 10_000),
  })
}

export function useContainerStatus(id: string) {
  return useQuery({
    queryKey: ['projects', id, 'status'],
    queryFn: () => api<ContainerStatus>(`/projects/${id}/status`),
    refetchInterval: 10_000,
  })
}

export function useDeployment(id: string) {
  return useQuery({
    queryKey: ['deployments', id],
    queryFn: () => api<DeploymentDetail>(`/deployments/${id}`),
    refetchInterval: (q) => (q.state.data && isActive(q.state.data.status) ? 2_000 : false),
  })
}

export function useCreateProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateProjectInput) => api<Project>('/projects', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'], exact: true }),
  })
}

export function useDeploy(projectId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<Deployment>(`/projects/${projectId}/deploy`, { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  })
}

export function useCancelDeployment(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>(`/deployments/${id}/cancel`, { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['deployments', id] }),
  })
}

export function useDeleteProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<{ warning: string }>(`/projects/${id}`, { method: 'DELETE' }),
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: ['projects', id] })
      qc.invalidateQueries({ queryKey: ['projects'], exact: true })
    },
  })
}
