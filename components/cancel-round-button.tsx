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
  const [isConfirmOpen, setIsConfirmOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isCancelling, setIsCancelling] = useState(false)

  async function executeCancel() {
    setError(null)
    setIsCancelling(true)
    setIsConfirmOpen(false)

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

  const buttonClassName =
    variant === 'danger'
      ? 'rounded-xl border border-red-200 px-4 py-3 text-base font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60'
      : 'rounded-xl border border-red-200 px-4 py-3 text-sm font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60'

  return (
    <>
      <div className="space-y-2">
        <button
          type="button"
          onClick={() => setIsConfirmOpen(true)}
          disabled={isCancelling}
          className={buttonClassName}
        >
          {isCancelling ? 'Cancelling…' : label}
        </button>
        {error ? <p className="text-sm font-medium text-red-600">{error}</p> : null}
      </div>

      {isConfirmOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={() => setIsConfirmOpen(false)}
        >
          <div
            className="w-full max-w-sm rounded-2xl bg-white p-6 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-lg font-semibold text-stone-900">Cancel this round?</h2>
            <p className="mt-2 text-sm text-stone-600">
              All progress for this round will be permanently deleted. This cannot be undone.
            </p>
            <div className="mt-6 flex gap-3">
              <button
                type="button"
                onClick={() => setIsConfirmOpen(false)}
                className="flex-1 rounded-xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
              >
                Keep Playing
              </button>
              <button
                type="button"
                onClick={executeCancel}
                className="flex-1 rounded-xl bg-red-600 px-4 py-3 text-sm font-semibold text-white transition hover:bg-red-700"
              >
                Yes, Cancel Round
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
