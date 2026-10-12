import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, ApiError } from '../lib/api'
import { useApp } from '../store'
import { AuthCard, Button, ErrorText, Spinner } from '../components/ui'

interface Preview {
  code: string
  kind: 'registration' | 'group'
  valid: boolean
  group?: { name: string; member_count: number }
}

export default function InvitePage() {
  const { code = '' } = useParams()
  const navigate = useNavigate()
  const me = useApp((s) => s.me)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .get<Preview>(`/api/v1/invites/${encodeURIComponent(code)}`)
      .then(setPreview)
      .catch(() => setError('This invite link is not valid.'))
  }, [code])

  async function join() {
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ group: { id: string }; channels: { id: string }[] }>(
        `/api/v1/invites/${encodeURIComponent(code)}/join`,
      )
      navigate(`/app/g/${res.group.id}/${res.channels[0]?.id ?? ''}`, { replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        navigate('/app', { replace: true })
        return
      }
      setError(err instanceof ApiError ? err.message : 'Could not join')
    } finally {
      setBusy(false)
    }
  }

  if (error && !preview) {
    return (
      <AuthCard title="Invite">
        <ErrorText>{error}</ErrorText>
        <Link to="/" className="mt-4 block text-center text-sm text-accent hover:underline">Go home</Link>
      </AuthCard>
    )
  }
  if (!preview) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    )
  }

  const title = preview.group ? `Join ${preview.group.name}` : 'Join this server'
  return (
    <AuthCard title={title}>
      {preview.group && (
        <p className="mb-4 text-center text-sm text-ink-1">
          {preview.group.member_count} member{preview.group.member_count === 1 ? '' : 's'}
        </p>
      )}
      {!preview.valid ? (
        <ErrorText>This invite has expired or been revoked.</ErrorText>
      ) : me ? (
        <>
          <ErrorText>{error}</ErrorText>
          <Button className="w-full" onClick={join} disabled={busy}>
            {busy ? 'Joining…' : 'Accept invite'}
          </Button>
        </>
      ) : (
        <div className="space-y-3">
          <p className="text-center text-sm text-ink-1">Sign in or create an account to accept.</p>
          <Link to={`/register?invite=${encodeURIComponent(code)}`}>
            <Button className="w-full">Create account</Button>
          </Link>
          <Link to="/login">
            <Button variant="ghost" className="w-full">Sign in</Button>
          </Link>
        </div>
      )}
    </AuthCard>
  )
}
