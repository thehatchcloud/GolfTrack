'use client'

import { useRouter } from 'next/navigation'
import { useState } from 'react'

type ReviewRoundFormProps = {
  roundId: number
  initialNote: string | null
}

export function ReviewRoundForm({ roundId, initialNote }: ReviewRoundFormProps) {
  const router = useRouter()
  const [note, setNote] = useState(initialNote ?? '')
  const [error, setError] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setIsSubmitting(true)

    try {
      const response = await fetch(`/api/rounds/${roundId}/complete`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ note }),
      })

      const body = (await response.json().catch(() => null)) as { id?: number; error?: string } | null

      if (!response.ok || !body?.id) {
        throw new Error(body?.error ?? 'Unable to complete round')
      }

      router.push(`/rounds/${roundId}`)
      router.refresh()
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : 'Unable to complete round')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4 rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
      <div>
        <h2 className="text-lg font-semibold text-stone-900">Round notes</h2>
        <p className="mt-1 text-sm text-stone-600">Add any comments from the round before you save it.</p>
      </div>

      <textarea
        value={note}
        onChange={(event) => setNote(event.target.value)}
        rows={5}
        maxLength={1000}
        placeholder="What went well? Any clubs to remember?"
        className="w-full rounded-2xl border border-stone-300 px-4 py-3 text-base outline-none focus:border-emerald-600"
      />

      {error ? <p className="text-sm font-medium text-red-600">{error}</p> : null}

      <div className="flex flex-col gap-3 sm:flex-row">
        <button
          type="submit"
          disabled={isSubmitting}
          className="rounded-xl bg-emerald-700 px-4 py-3 text-base font-semibold text-white transition hover:bg-emerald-800 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {isSubmitting ? 'Saving…' : 'Submit Round'}
        </button>
        <button
          type="button"
          onClick={() => router.push(`/rounds/${roundId}/play`)}
          className="rounded-xl border border-stone-300 px-4 py-3 text-base font-medium text-stone-700 transition hover:bg-stone-50"
        >
          Back to Play
        </button>
      </div>
    </form>
  )
}
