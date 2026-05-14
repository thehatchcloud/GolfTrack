'use client'

import { useRouter } from 'next/navigation'
import { useState } from 'react'

type CancelRoundButtonProps = {
  roundId: number
  redirectTo?: string
  variant?: 'inline' | 'danger'
  label?: string
}

export function CancelRoundButton({
  roundId,
  redirectTo = '/',
  variant = 'inline',
  label = 'Cancel Round',
}: CancelRoundButtonProps) {
  const router = useRouter()
  const [error, setError] = useState<string | null>(null)
  const [isCancelling, setIsCancelling] = useState(false)

  async function handleCancel() {
    const confirmed = window.confirm('Cancel this round? All progress for this round will be deleted.')
    if (!confirmed) return

    setError(null)
    setIsCancelling(true)

    try {
      const response = await fetch(`/api/rounds/${roundId}/cancel`, {
        method: 'POST',
      })

      const body = (await response.json().catch(() => null)) as { error?: string } | null
      if (!response.ok) {
        throw new Error(body?.error ?? 'Unable to cancel round')
      }

      router.push(redirectTo)
      router.refresh()
    } catch (cancelError) {
      setError(cancelError instanceof Error ? cancelError.message : 'Unable to cancel round')
    } finally {
      setIsCancelling(false)
    }
  }

  const className =
    variant === 'danger'
      ? 'rounded-xl border border-red-200 px-4 py-3 text-base font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60'
      : 'rounded-xl border border-red-200 px-4 py-3 text-sm font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60'

  return (
    <div className="space-y-2">
      <button type="button" onClick={handleCancel} disabled={isCancelling} className={className}>
        {isCancelling ? 'Cancelling…' : label}
      </button>
      {error ? <p className="text-sm font-medium text-red-600">{error}</p> : null}
    </div>
  )
}
