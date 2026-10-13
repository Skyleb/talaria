// Lightbox: full-screen image viewer with zoom. Scroll wheel or pinch to
// zoom, drag to pan, double-tap/double-click to toggle 1x / 2.5x, +/- keys,
// Esc to close.
import { useEffect, useRef, useState, type PointerEvent as RPointerEvent, type WheelEvent as RWheelEvent } from 'react'
import { Download, Minus, Plus, RotateCcw, X } from 'lucide-react'

const MIN = 1
const MAX = 8

export function Lightbox({ url, alt, onClose }: { url: string; alt?: string; onClose: () => void }) {
  const [scale, setScale] = useState(1)
  const [pos, setPos] = useState({ x: 0, y: 0 })
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const pinch = useRef<{ dist: number; scale: number } | null>(null)
  const drag = useRef<{ x: number; y: number; px: number; py: number } | null>(null)
  const lastTap = useRef(0)
  const moved = useRef(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if (e.key === '+' || e.key === '=') zoomTo(scale * 1.25)
      if (e.key === '-') zoomTo(scale / 1.25)
      if (e.key === '0') reset()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  function clamp(n: number, lo: number, hi: number) {
    return Math.min(hi, Math.max(lo, n))
  }

  function zoomTo(next: number, around?: { x: number; y: number }) {
    const s = clamp(next, MIN, MAX)
    if (s === 1) {
      setScale(1)
      setPos({ x: 0, y: 0 })
      return
    }
    if (around) {
      // Keep the point under the cursor fixed while zooming.
      const k = s / scale
      setPos((p) => ({ x: around.x - (around.x - p.x) * k, y: around.y - (around.y - p.y) * k }))
    }
    setScale(s)
  }

  function reset() {
    setScale(1)
    setPos({ x: 0, y: 0 })
  }

  // Coordinates relative to the viewport centre (where the image is anchored).
  function rel(e: { clientX: number; clientY: number }) {
    return { x: e.clientX - window.innerWidth / 2, y: e.clientY - window.innerHeight / 2 }
  }

  function onWheel(e: RWheelEvent) {
    e.preventDefault()
    const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15
    zoomTo(scale * factor, rel(e))
  }

  function onPointerDown(e: RPointerEvent) {
    ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    moved.current = false
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      pinch.current = { dist: Math.hypot(a.x - b.x, a.y - b.y), scale }
      drag.current = null
    } else if (pointers.current.size === 1) {
      drag.current = { x: e.clientX, y: e.clientY, px: pos.x, py: pos.y }
    }
  }

  function onPointerMove(e: RPointerEvent) {
    if (!pointers.current.has(e.pointerId)) return
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pointers.current.size === 2 && pinch.current) {
      const [a, b] = [...pointers.current.values()]
      const dist = Math.hypot(a.x - b.x, a.y - b.y)
      const mid = rel({ clientX: (a.x + b.x) / 2, clientY: (a.y + b.y) / 2 })
      zoomTo(pinch.current.scale * (dist / pinch.current.dist), mid)
      moved.current = true
      return
    }
    if (drag.current && scale > 1) {
      const dx = e.clientX - drag.current.x
      const dy = e.clientY - drag.current.y
      if (Math.abs(dx) + Math.abs(dy) > 3) moved.current = true
      setPos({ x: drag.current.px + dx, y: drag.current.py + dy })
    }
  }

  function onPointerUp(e: RPointerEvent) {
    pointers.current.delete(e.pointerId)
    if (pointers.current.size < 2) pinch.current = null
    if (pointers.current.size === 0) {
      drag.current = null
      if (!moved.current) {
        const now = Date.now()
        if (now - lastTap.current < 300) {
          // Double tap: toggle between fit and 2.5x around the tap point.
          scale > 1 ? reset() : zoomTo(2.5, rel(e))
          lastTap.current = 0
        } else {
          lastTap.current = now
        }
      }
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 select-none overflow-hidden bg-black/90"
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      style={{ touchAction: 'none', cursor: scale > 1 ? 'grab' : 'zoom-in' }}
    >
      <img
        src={url}
        alt={alt ?? ''}
        draggable={false}
        className="absolute left-1/2 top-1/2 max-h-[calc(100vh-2rem)] max-w-[calc(100vw-2rem)] object-contain"
        style={{
          transform: `translate(-50%, -50%) translate(${pos.x}px, ${pos.y}px) scale(${scale})`,
          transition: pointers.current.size ? 'none' : 'transform 120ms ease-out',
        }}
      />
      <div
        className="absolute right-3 top-3 flex items-center gap-1 rounded-full bg-black/60 p-1 text-white"
        onPointerDown={(e) => e.stopPropagation()}
        onWheel={(e) => e.stopPropagation()}
      >
        <Ctl title="Zoom out (-)" onClick={() => zoomTo(scale / 1.25)}>
          <Minus size={18} />
        </Ctl>
        <span className="min-w-12 text-center text-xs tabular-nums">{Math.round(scale * 100)}%</span>
        <Ctl title="Zoom in (+)" onClick={() => zoomTo(scale * 1.25)}>
          <Plus size={18} />
        </Ctl>
        <Ctl title="Fit to screen (0)" onClick={reset}>
          <RotateCcw size={18} />
        </Ctl>
        <a href={url} download title="Download" className="rounded-full p-2 hover:bg-white/20">
          <Download size={18} />
        </a>
        <Ctl title="Close (Esc)" onClick={onClose}>
          <X size={18} />
        </Ctl>
      </div>
      {scale === 1 && (
        <p className="pointer-events-none absolute bottom-4 left-0 right-0 text-center text-xs text-white/60">
          Scroll or pinch to zoom · double-tap to zoom in · drag to move
        </p>
      )}
    </div>
  )
}

function Ctl({ title, onClick, children }: { title: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button title={title} onClick={onClick} className="rounded-full p-2 hover:bg-white/20">
      {children}
    </button>
  )
}
