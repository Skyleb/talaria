// AvatarCropper: pick the part of an image to use as a profile picture or
// group icon. Drag to move, slider (or wheel) to zoom; the result is a
// square 512px image.
import { useEffect, useRef, useState } from 'react'
import { ZoomIn, ZoomOut } from 'lucide-react'
import { Button, ErrorText, Modal } from './ui'

const VIEW = 260 // on-screen crop square, px
const OUT = 512 // output size, px

export function AvatarCropper({
  file,
  title = 'Adjust your picture',
  round = true,
  onCancel,
  onDone,
}: {
  file: File | null
  title?: string
  round?: boolean
  onCancel: () => void
  onDone: (blob: Blob) => void | Promise<void>
}) {
  const [url, setUrl] = useState<string | null>(null)
  const [nat, setNat] = useState<{ w: number; h: number } | null>(null)
  const [zoom, setZoom] = useState(1) // multiplier on top of "cover" scale
  const [offset, setOffset] = useState({ x: 0, y: 0 }) // image top-left within the view
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const drag = useRef<{ px: number; py: number; ox: number; oy: number } | null>(null)
  const imgRef = useRef<HTMLImageElement>(null)

  useEffect(() => {
    if (!file) {
      setUrl(null)
      setNat(null)
      return
    }
    const u = URL.createObjectURL(file)
    setUrl(u)
    setZoom(1)
    setError('')
    const img = new Image()
    img.onload = () => {
      setNat({ w: img.naturalWidth, h: img.naturalHeight })
      // Start centered at cover scale.
      const s = Math.max(VIEW / img.naturalWidth, VIEW / img.naturalHeight)
      setOffset({ x: (VIEW - img.naturalWidth * s) / 2, y: (VIEW - img.naturalHeight * s) / 2 })
    }
    img.onerror = () => setError('That file does not look like an image.')
    img.src = u
    return () => URL.revokeObjectURL(u)
  }, [file])

  if (!file) return null
  const cover = nat ? Math.max(VIEW / nat.w, VIEW / nat.h) : 1
  const scale = cover * zoom
  const dispW = nat ? nat.w * scale : VIEW
  const dispH = nat ? nat.h * scale : VIEW

  // Keep the image covering the whole square.
  function clamp(o: { x: number; y: number }, w = dispW, h = dispH) {
    return {
      x: Math.min(0, Math.max(VIEW - w, o.x)),
      y: Math.min(0, Math.max(VIEW - h, o.y)),
    }
  }

  function setZoomKeepingCenter(z: number) {
    if (!nat) return
    const next = Math.min(4, Math.max(1, z))
    const s0 = cover * zoom
    const s1 = cover * next
    // Keep the image point under the view's center fixed.
    const cx = (VIEW / 2 - offset.x) / s0
    const cy = (VIEW / 2 - offset.y) / s0
    const o = { x: VIEW / 2 - cx * s1, y: VIEW / 2 - cy * s1 }
    setZoom(next)
    setOffset(clamp(o, nat.w * s1, nat.h * s1))
  }

  async function save() {
    const img = imgRef.current
    if (!img || !nat) return
    setBusy(true)
    setError('')
    try {
      const canvas = document.createElement('canvas')
      canvas.width = OUT
      canvas.height = OUT
      const ctx = canvas.getContext('2d')!
      ctx.imageSmoothingQuality = 'high'
      const sx = -offset.x / scale
      const sy = -offset.y / scale
      const sw = VIEW / scale
      ctx.drawImage(img, sx, sy, sw, sw, 0, 0, OUT, OUT)
      const type = file!.type === 'image/png' ? 'image/png' : 'image/jpeg'
      const blob = await new Promise<Blob | null>((res) => canvas.toBlob(res, type, 0.9))
      if (!blob) throw new Error('Could not process the image')
      await onDone(blob)
    } catch (e: any) {
      setError(e?.message ?? 'Could not save the picture')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open onClose={onCancel} title={title}>
      <p className="mb-3 text-sm text-ink-1">Drag to move. Pinch, scroll, or use the slider to zoom.</p>
      <div className="flex justify-center">
        <div
          className={`relative overflow-hidden bg-bg-2 ${round ? 'rounded-full' : 'rounded-2xl'} touch-none select-none`}
          style={{ width: VIEW, height: VIEW, cursor: 'grab' }}
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId)
            drag.current = { px: e.clientX, py: e.clientY, ox: offset.x, oy: offset.y }
          }}
          onPointerMove={(e) => {
            if (!drag.current) return
            setOffset(clamp({ x: drag.current.ox + (e.clientX - drag.current.px), y: drag.current.oy + (e.clientY - drag.current.py) }))
          }}
          onPointerUp={() => (drag.current = null)}
          onPointerCancel={() => (drag.current = null)}
          onWheel={(e) => {
            e.preventDefault()
            setZoomKeepingCenter(zoom * (e.deltaY < 0 ? 1.08 : 1 / 1.08))
          }}
        >
          {url && (
            <img
              ref={imgRef}
              src={url}
              alt=""
              draggable={false}
              className="pointer-events-none absolute max-w-none"
              style={{ left: offset.x, top: offset.y, width: dispW, height: dispH }}
            />
          )}
        </div>
      </div>
      <div className="mt-3 flex items-center gap-2 px-2">
        <ZoomOut size={16} className="text-ink-2" />
        <input
          type="range"
          min={1}
          max={4}
          step={0.01}
          value={zoom}
          onChange={(e) => setZoomKeepingCenter(Number(e.target.value))}
          className="flex-1 accent-[var(--accent)]"
          aria-label="Zoom"
        />
        <ZoomIn size={16} className="text-ink-2" />
      </div>
      <ErrorText>{error}</ErrorText>
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button onClick={save} disabled={busy || !nat}>
          {busy ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </Modal>
  )
}
