// Lightbox: full-screen image/video viewer with zoom. Scroll wheel or pinch
// to zoom, drag to pan, double-tap/double-click to toggle 1x / 2.5x, +/-
// keys, Esc to close. Given a list of items it becomes a gallery: ←/→ keys,
// on-screen arrows, and a horizontal swipe (when not zoomed) move between
// them.
import { useEffect, useRef, useState, type PointerEvent as RPointerEvent } from 'react'
import { ChevronLeft, ChevronRight, Download, Minus, Plus, RotateCcw, X } from 'lucide-react'

const MIN = 1
const MAX = 8

export interface LightboxItem {
  url: string
  alt?: string
  kind: 'image' | 'video'
}

export function Lightbox({
  url,
  alt,
  items,
  index = 0,
  onIndexChange,
  onClose,
}: {
  url?: string
  alt?: string
  items?: LightboxItem[]
  index?: number
  onIndexChange?: (i: number) => void
  onClose: () => void
}) {
  const list: LightboxItem[] = items ?? [{ url: url ?? '', alt, kind: 'image' }]
  const [i, setI] = useState(Math.min(Math.max(index, 0), list.length - 1))
  const current = list[i] ?? list[0]
  const isVideo = current.kind === 'video'
  const [scale, setScale] = useState(1)
  const [pos, setPos] = useState({ x: 0, y: 0 })
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const pinch = useRef<{ dist: number; scale: number } | null>(null)
  const drag = useRef<{ x: number; y: number; px: number; py: number } | null>(null)
  const lastTap = useRef(0)
  const moved = useRef(false)
  const swipe = useRef<{ x: number; y: number } | null>(null)
  const box = useRef<HTMLDivElement>(null)
  const scaleRef = useRef(scale)
  scaleRef.current = scale

  function goTo(next: number) {
    if (next < 0 || next >= list.length || next === i) return
    setI(next)
    setScale(1)
    setPos({ x: 0, y: 0 })
    onIndexChange?.(next)
  }

  // React registers wheel listeners as passive, which blocks preventDefault,
  // so the page behind would scroll. Attach a non-passive one by hand.
  useEffect(() => {
    const el = box.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      if (isVideo) return
      const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15
      zoomTo(scaleRef.current * factor, rel(e))
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isVideo])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if (e.key === 'ArrowLeft') goTo(i - 1)
      if (e.key === 'ArrowRight') goTo(i + 1)
      if (isVideo) return
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
      const k = s / scaleRef.current
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

  function onPointerDown(e: RPointerEvent) {
    ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    moved.current = false
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      pinch.current = { dist: Math.hypot(a.x - b.x, a.y - b.y), scale }
      drag.current = null
      swipe.current = null
    } else if (pointers.current.size === 1) {
      drag.current = { x: e.clientX, y: e.clientY, px: pos.x, py: pos.y }
      swipe.current = scale === 1 && list.length > 1 ? { x: e.clientX, y: e.clientY } : null
    }
  }

  function onPointerMove(e: RPointerEvent) {
    if (!pointers.current.has(e.pointerId)) return
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (isVideo) return
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
      // Horizontal swipe between gallery items (only when not zoomed).
      if (swipe.current && e.pointerType !== 'mouse') {
        const dx = e.clientX - swipe.current.x
        const dy = e.clientY - swipe.current.y
        swipe.current = null
        if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) {
          goTo(dx < 0 ? i + 1 : i - 1)
          return
        }
      }
      if (!moved.current && !isVideo) {
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

  const mediaClass = 'absolute left-1/2 top-1/2 max-h-[calc(100vh-2rem)] max-w-[calc(100vw-2rem)] object-contain'
  const stop = { onPointerDown: (e: RPointerEvent) => e.stopPropagation(), onWheel: (e: React.WheelEvent) => e.stopPropagation() }

  return (
    <div
      ref={box}
      className="fixed inset-0 z-50 select-none overflow-hidden bg-black/90"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      style={{ touchAction: 'none', cursor: isVideo ? 'default' : scale > 1 ? 'grab' : 'zoom-in' }}
    >
      {isVideo ? (
        <video
          key={current.url}
          src={current.url}
          controls
          autoPlay
          playsInline
          className={`${mediaClass} -translate-x-1/2 -translate-y-1/2`}
          {...stop}
        />
      ) : (
        <img
          key={current.url}
          src={current.url}
          alt={current.alt ?? ''}
          draggable={false}
          className={mediaClass}
          style={{
            transform: `translate(-50%, -50%) translate(${pos.x}px, ${pos.y}px) scale(${scale})`,
            transition: pointers.current.size ? 'none' : 'transform 120ms ease-out',
          }}
        />
      )}
      {list.length > 1 && (
        <>
          {i > 0 && (
            <button
              title="Previous (←)"
              onClick={() => goTo(i - 1)}
              className="absolute left-2 top-1/2 -translate-y-1/2 rounded-full bg-black/60 p-2 text-white hover:bg-black/80"
              {...stop}
            >
              <ChevronLeft size={24} />
            </button>
          )}
          {i < list.length - 1 && (
            <button
              title="Next (→)"
              onClick={() => goTo(i + 1)}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded-full bg-black/60 p-2 text-white hover:bg-black/80"
              {...stop}
            >
              <ChevronRight size={24} />
            </button>
          )}
          <span className="pointer-events-none absolute left-3 top-4 rounded-full bg-black/60 px-2 py-1 text-xs tabular-nums text-white">
            {i + 1} / {list.length}
          </span>
        </>
      )}
      <div className="absolute right-3 top-3 flex items-center gap-1 rounded-full bg-black/60 p-1 text-white" {...stop}>
        {!isVideo && (
          <>
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
          </>
        )}
        <a href={current.url} download title="Download" className="rounded-full p-2 hover:bg-white/20">
          <Download size={18} />
        </a>
        <Ctl title="Close (Esc)" onClick={onClose}>
          <X size={18} />
        </Ctl>
      </div>
      {scale === 1 && !isVideo && (
        <p className="pointer-events-none absolute bottom-4 left-0 right-0 text-center text-xs text-white/60">
          Scroll or pinch to zoom · double-tap to zoom in · drag to move
          {list.length > 1 ? ' · swipe or ← → for more' : ''}
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
