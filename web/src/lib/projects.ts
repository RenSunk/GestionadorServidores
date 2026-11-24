import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api.ts'

export type BuildType = 'dockerfile' | 'runtime'

export type Runtime = {
  id: string
  label: string
  versions: string[] | null
  defaultVersion: string
  defaultPort: number
  needsBaseImage: boolean
  buildPlaceholder: string
  startPlaceholder: string
}

export type Detection = {
  hasDockerfile: boolean
  runtime: string
  framework: string
  version: string
  packageManager: string
  buildCommand: string
  startCommand: string
  port: number
  notes: string[]
}

export type DetectInput = {
  repoUrl: string
  branch: string
  gitUsername?: string
  gitToken?: string
  rootDir?: string
  dockerfile?: string
}

export type BuildSpec = {
  runtime: string
  version: string
  baseImage: string
  buildCommand: string
  startCommand: string
  port: number
  buildArgs?: string[]
}

export type EnvVar = { key: string; value: string; build: boolean }

export type PendingChanges = { build: boolean; runtime: boolean }

export type DeploymentStatus = 'queued' | 'building' | 'deploying' | 'succeeded' | 'failed' | 'canceled'

export type Deployment = {
  id: string
  projectId: string
  status: DeploymentStatus
  commitSha: string
  commitMessage: string
  image: string
  /** Si no está vacío, este deploy reutilizó la imagen de otro (rollback o reinicio). */
  sourceDeploymentId: string
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
  buildType: BuildType
  runtime: string
  runtimeVersion: string
  runtimeLabel: string
  baseImage: string
  buildCommand: string
  startCommand: string
  cpuLimit: number | null
  memoryLimitMb: number | null
  url: string
  containerName: string
  createdAt: string
  lastDeployment: Deployment | null
  activeDeploymentId: string
}

export type ProjectDetail = Project & { deployments: Deployment[]; pending: PendingChanges }

export type ContainerStatus = {
  state: 'running' | 'exited' | 'restarting' | 'created' | 'paused' | 'dead' | 'missing' | 'unknown'
  startedAt?: string | null
  restartCount?: number
  deploymentId?: string
  cpuPercent?: number
  memoryUsage?: string
  memoryPercent?: number
  netIO?: string
  /** IDs de deploys cuya imagen sigue en el servidor. */
  images?: string[]
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
  buildType: BuildType
  runtime?: string
  runtimeVersion?: string
  baseImage?: string
  buildCommand?: string
  startCommand?: string
}

export type UpdateProjectInput = Omit<CreateProjectInput, 'serverId'> & {
  clearToken?: boolean
  cpuLimit: number | null
  memoryLimitMb: number | null
}

/** "usuario/repo" a partir de la URL del repositorio. */
export function repoName(url: string) {
  return url.replace(/^https?:\/\/[^/]+\//, '').replace(/\.git$/, '')
}

export function isActive(status: DeploymentStatus) {
  return status === 'queued' || status === 'building' || status === 'deploying'
}

export function useRuntimes() {
  return useQuery({
    queryKey: ['runtimes'],
    queryFn: () => api<Runtime[]>('/runtimes'),
    staleTime: Infinity,
  })
}

export function useDetect() {
  return useMutation({
    mutationFn: (input: DetectInput) => api<Detection>('/projects/detect', { method: 'POST', body: JSON.stringify(input) }),
  })
}

/** Vista previa del Dockerfile generado para una configuración (sin guardar). */
export function useDockerfilePreview(spec: BuildSpec, enabled: boolean) {
  return useQuery({
    queryKey: ['dockerfile-preview', spec],
    queryFn: () => api<{ dockerfile: string }>('/dockerfile-preview', { method: 'POST', body: JSON.stringify(spec) }),
    enabled,
    retry: false,
  })
}

export function useProjectDockerfile(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['projects', id, 'dockerfile'],
    queryFn: () => api<{ dockerfile: string }>(`/projects/${id}/dockerfile`),
    enabled,
  })
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

export function useUpdateProject(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdateProjectInput) => api<Project>(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  })
}

export function useEnv(id: string) {
  return useQuery({
    queryKey: ['projects', id, 'env'],
    queryFn: () => api<{ vars: EnvVar[] }>(`/projects/${id}/env`),
    select: (d) => d.vars,
  })
}

export function useSetEnv(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars: EnvVar[]) => api<{ vars: EnvVar[] }>(`/projects/${id}/env`, { method: 'PUT', body: JSON.stringify({ vars }) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects', id] }),
  })
}

/** Recrea el contenedor con la imagen actual para aplicar la configuración nueva. */
export function useRestart(projectId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<Deployment>(`/projects/${projectId}/restart`, { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  })
}

/** Vuelve a publicar la imagen de un deploy anterior (rollback). */
export function useRedeploy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (deploymentId: string) => api<Deployment>(`/deployments/${deploymentId}/redeploy`, { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  })
}
