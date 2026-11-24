import type { ReactNode } from 'react'

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="text-sm text-zinc-300">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-zinc-500">{hint}</span>}
    </label>
  )
}

export function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <div className="mt-3 rounded-md border border-[#d03b3b]/50 bg-[#d03b3b]/10 p-3 text-sm text-zinc-200">
      <span className="mr-1.5 text-[#d03b3b]">✕</span>
      {children}
    </div>
  )
}

export function Choice(props: { checked: boolean; disabled?: boolean; onChange: () => void; title: string; hint: string }) {
  return (
    <label
      className={`rounded-md border p-3 text-sm ${
        props.disabled
          ? 'cursor-not-allowed border-zinc-800 opacity-50'
          : props.checked
            ? 'cursor-pointer border-zinc-400 bg-zinc-900'
            : 'cursor-pointer border-zinc-800 hover:border-zinc-700'
      }`}
    >
      <input type="radio" checked={props.checked} disabled={props.disabled} onChange={props.onChange} className="sr-only" />
      <div className="font-medium">{props.title}</div>
      <div className="mt-1 text-xs text-zinc-500">{props.hint}</div>
    </label>
  )
}
