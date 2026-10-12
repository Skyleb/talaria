import { useState } from 'react'
import { Download, FileText, X } from 'lucide-react'
import { fileUrl } from '../../lib/api'
import { formatBytes } from '../../lib/time'
import type { Attachment } from '../../lib/types'

export function AttachmentView({ attachment: a }: { attachment: Attachment }) {
  const url = fileUrl(a)
  if (a.mime.startsWith('image/')) return <ImageAttachment a={a} url={url} />
  if (a.mime.startsWith('video/')) {
    return (
      <video
        controls
        preload="metadata"
        className="mt-1 max-h-80 max-w-full rounded-lg border border-edge bg-black sm:max-w-md"
        src={url}
      />
    )
  }
  if (a.mime.startsWith('audio/')) {
    return (
      <div className="mt-1 max-w-md rounded-lg border border-edge bg-bg-2 p-2">
        <div className="mb-1 truncate text-xs text-ink-1">{a.file_name}</div>
        <audio controls preload="metadata" className="w-full" src={url} />
      </div>
    )
  }
  return (
    <a
      href={url}
      download={a.file_name}
      className="mt-1 flex max-w-md items-center gap-3 rounded-lg border border-edge bg-bg-2 p-3 hover:bg-bg-3"
    >
      <FileText size={28} className="shrink-0 text-accent" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-ink">{a.file_name}</div>
        <div className="text-xs text-ink-2">{formatBytes(a.size_bytes)}</div>
      </div>
      <Download size={18} className="shrink-0 text-ink-1" />
    </a>
  )
}

function ImageAttachment({ a, url }: { a: Attachment; url: string }) {
  const [open, setOpen] = useState(false)
  const ratio = a.width && a.height ? a.width / a.height : undefined
  return (
    <>
      <button onClick={() => setOpen(true)} className="mt-1 block">
        <img
          src={url}
          alt={a.file_name}
          loading="lazy"
          className="max-h-80 max-w-full rounded-lg border border-edge object-contain sm:max-w-md"
          style={ratio ? { aspectRatio: String(ratio) } : undefined}
        />
      </button>
      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-4" onClick={() => setOpen(false)}>
          <button className="absolute right-4 top-4 rounded-full bg-black/50 p-2 text-white">
            <X size={20} />
          </button>
          <img src={url} alt={a.file_name} className="max-h-full max-w-full object-contain" onClick={(e) => e.stopPropagation()} />
        </div>
      )}
    </>
  )
}
