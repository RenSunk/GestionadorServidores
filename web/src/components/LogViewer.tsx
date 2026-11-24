import { useEffect, useRef, useState } from 'react'

const MAX_LINES = 5000
// Quita secuencias de color ANSI (\x1b[...m) que algunas apps escriben en sus logs.
// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[A-Za-z]/g

type Props = {
  /** URL del endpoint SSE. Cambiarla reinicia la conexión. */
  url: string
  /** Se llama con el dato del evento "end" (estado final o motivo). */
  onEnd?: (data: string) => void
  emptyText?: string
  height?: string
}

type Conn = 'connecting' | 'open' | 'ended' | 'error'

// Visor de logs en vivo vía Server-Sent Events. Hace autoscroll mientras el
// usuario esté al final; si sube para leer, deja de desplazarse.
// key={url}: si cambia la URL, se monta de nuevo con el estado limpio.
export function LogViewer(props: Props) {
  return <LogStream key={props.url} {...props} />
}

function LogStream({ url, onEnd, emptyText = 'Sin líneas todavía…', height = 'h-[480px]' }: Props) {
  const [lines, setLines] = useState<string[]>([])
  const [conn, setConn] = useState<Conn>('connecting')
  const boxRef = useRef<HTMLDivElement>(null)
  const stickRef = useRef(true)
  const onEndRef = useRef(onEnd)

  useEffect(() => {
    onEndRef.current = onEnd
  }, [onEnd])

  useEffect(() => {
    const es = new EventSource(url)
    let pending: string[] = []
    let frame = 0
    // Agrupamos líneas por frame: un build puede emitir cientos por segundo.
    const flush = () => {
      frame = 0
      const batch = pending
      pending = []
      setLines((prev) => {
        const next = prev.concat(batch)
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
      })
    }

    es.onopen = () => setConn('open')
    es.onmessage = (e) => {
      pending.push(e.data.replace(ANSI, ''))
      if (!frame) frame = requestAnimationFrame(flush)
    }
    es.addEventListener('end', (e) => {
      es.close()
      if (frame) cancelAnimationFrame(frame)
      flush()
      setConn('ended')
      onEndRef.current?.((e as MessageEvent).data)
    })
    es.onerror = () => {
      // EventSource reintenta solo; si el servidor cerró del todo, lo marcamos.
      if (es.readyState === EventSource.CLOSED) setConn('error')
    }

    return () => {
      es.close()
      if (frame) cancelAnimationFrame(frame)
    }
  }, [url])

  useEffect(() => {
    const box = boxRef.current
    if (box && stickRef.current) box.scrollTop = box.scrollHeight
  }, [lines])

  function onScroll() {
    const box = boxRef.current
    if (box) stickRef.current = box.scrollHeight - box.scrollTop - box.clientHeight < 40
  }

  return (
    <div className="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950">
      <div className="flex items-center justify-between border-b border-zinc-800 px-3 py-1.5 text-xs text-zinc-500">
        <span>{lines.length.toLocaleString('es')} líneas</span>
        <ConnLabel conn={conn} />
      </div>
      <div ref={boxRef} onScroll={onScroll} className={`${height} overflow-auto p-3 font-mono text-xs leading-relaxed`}>
        {lines.length === 0 ? (
          <div className="text-zinc-600">{emptyText}</div>
        ) : (
          lines.map((line, i) => (
            <div
              key={i}
              className={`whitespace-pre-wrap break-all ${
                line.startsWith('==> ERROR') ? 'text-[#e66767]' : line.startsWith('==>') ? 'font-semibold text-zinc-100' : 'text-zinc-400'
              }`}
            >
              {line || ' '}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

function ConnLabel({ conn }: { conn: Conn }) {
  switch (conn) {
    case 'connecting':
      return <span>Conectando…</span>
    case 'open':
      return (
        <span className="inline-flex items-center gap-1.5 text-zinc-300">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#0ca30c]" aria-hidden />
          En vivo
        </span>
      )
    case 'ended':
      return <span>Finalizado</span>
    case 'error':
      return <span className="text-[#e66767]">✕ Conexión perdida</span>
  }
}
