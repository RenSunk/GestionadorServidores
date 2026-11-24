// Cliente HTTP mínimo para la API del backend.
// Todas las rutas son relativas a /api, así funciona igual en dev (proxy de Vite)
// y en producción (mismo origen que el binario).

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })

  if (!res.ok) {
    const body = await res.json().catch(() => null)
    throw new ApiError(res.status, body?.error ?? res.statusText)
  }

  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export type Health = {
  status: 'ok' | 'degraded'
  version: string
  database: 'sqlite' | 'postgres'
}
