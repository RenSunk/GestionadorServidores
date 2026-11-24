import { useState } from 'react'

export function CopyButton({ text, label = 'Copiar' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)

  return (
    <button
      type="button"
      onClick={async () => {
        await navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }}
      className="shrink-0 rounded-md border border-zinc-700 px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800"
    >
      {copied ? 'Copiado ✓' : label}
    </button>
  )
}
