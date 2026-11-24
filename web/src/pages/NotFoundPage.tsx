import { Link } from 'react-router'

export function NotFoundPage() {
  return (
    <div>
      <h1 className="text-2xl font-semibold">Página no encontrada</h1>
      <Link to="/" className="mt-4 inline-block text-sm text-zinc-400 underline hover:text-zinc-200">
        Volver al dashboard
      </Link>
    </div>
  )
}
