// Typed fetch wrapper for the REST API.

export class ApiError extends Error {
  code: string
  status: number
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  })
  if (res.status === 204) return undefined as T
  let data: any = null
  try {
    data = await res.json()
  } catch {
    /* non-JSON error body */
  }
  if (!res.ok) {
    const err = data?.error
    throw new ApiError(res.status, err?.code ?? 'unknown', err?.message ?? res.statusText)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  patch: <T>(path: string, body: unknown) => request<T>('PATCH', path, body),
  put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

export function uploadFile(
  file: File,
  onProgress?: (frac: number) => void,
  durationMs?: number,
): Promise<{ attachment: import('./types').Attachment }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/v1/uploads')
    xhr.withCredentials = true
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total)
    }
    xhr.onload = () => {
      try {
        const data = JSON.parse(xhr.responseText)
        if (xhr.status >= 200 && xhr.status < 300) resolve(data)
        else reject(new ApiError(xhr.status, data?.error?.code ?? 'upload', data?.error?.message ?? 'upload failed'))
      } catch {
        reject(new ApiError(xhr.status, 'upload', 'upload failed'))
      }
    }
    xhr.onerror = () => reject(new ApiError(0, 'network', 'network error during upload'))
    const form = new FormData()
    // duration_ms must precede file: the server streams the file part.
    if (durationMs && durationMs > 0) form.append('duration_ms', String(Math.round(durationMs)))
    form.append('file', file)
    xhr.send(form)
  })
}

export function fileUrl(att: import('./types').Attachment): string {
  return `/files/${att.id}/${encodeURIComponent(att.file_name)}`
}

// thumbUrl: an aspect-preserving thumbnail of an image attachment (fits in
// size×size). Same access rule as the file; non-images 404.
export function thumbUrl(att: import('./types').Attachment, size = 256): string {
  return `/thumbs/${att.id}?s=${size}`
}

export function avatarUrl(fileId: string, size = 128): string {
  return `/avatars/${fileId}?s=${size}`
}
