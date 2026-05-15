'use client'

import { useRouter } from 'next/navigation'
import { useState } from 'react'

import { DEFAULT_CLUBS } from '@/lib/clubs'

type Shot = {
  id: number
  shotNumber: number
  club: string
}

type ShotListProps = {
  roundId?: number
  holeNumber?: number
  shots: Shot[]
  editable?: boolean
}

export function ShotList({ shots, roundId, holeNumber, editable = false }: ShotListProps) {
  const router = useRouter()
  const [editingShotId, setEditingShotId] = useState<number | null>(null)
  const [editingClub, setEditingClub] = useState('')
  const [busyShotId, setBusyShotId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function saveShot(shotId: number) {
    if (!roundId || !holeNumber) return

    setError(null)
    setBusyShotId(shotId)

    try {
      const response = await fetch(`/api/rounds/${roundId}/holes/${holeNumber}/shots/${shotId}`, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ club: editingClub }),
      })

      const body = (await response.json().catch(() => null)) as { error?: string } | null
      if (!response.ok) {
        throw new Error(body?.error ?? 'Unable to update shot')
      }

      setEditingShotId(null)
      router.refresh()
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : 'Unable to update shot')
    } finally {
      setBusyShotId(null)
    }
  }

  async function removeShot(shotId: number) {
    if (!roundId || !holeNumber) return

    setError(null)
    setBusyShotId(shotId)

    try {
      const response = await fetch(`/api/rounds/${roundId}/holes/${holeNumber}/shots/${shotId}`, {
        method: 'DELETE',
      })

      const body = (await response.json().catch(() => null)) as { error?: string } | null
      if (!response.ok) {
        throw new Error(body?.error ?? 'Unable to delete shot')
      }

      router.refresh()
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : 'Unable to delete shot')
    } finally {
      setBusyShotId(null)
    }
  }

  function startEditing(shot: Shot) {
    setError(null)
    setEditingShotId(shot.id)
    setEditingClub(shot.club)
  }

  return (
    <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold text-stone-900">Shot history</h2>
          {editable ? <p className="mt-1 text-sm text-stone-600">Edit or remove any shot on this hole.</p> : null}
        </div>
        <span className="rounded-full bg-stone-100 px-3 py-1 text-xs font-medium text-stone-700">
          {shots.length} shots
        </span>
      </div>

      {error ? (
        <div className="mb-4 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm font-medium text-red-700">
          {error}
        </div>
      ) : null}

      {shots.length === 0 ? (
        <p className="text-sm text-stone-600">No shots yet. Tap a club below to record your first shot.</p>
      ) : (
        <ol className="space-y-3">
          {shots.map((shot) => {
            const isEditing = editingShotId === shot.id
            const isBusy = busyShotId === shot.id

            return (
              <li key={shot.id} className="rounded-xl border border-stone-200 px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm font-medium text-stone-700">Shot {shot.shotNumber}</span>
                  {!isEditing ? (
                    <span className="rounded-full bg-emerald-50 px-3 py-1 text-sm font-semibold text-emerald-800">
                      {shot.club}
                    </span>
                  ) : null}
                </div>

                {editable ? (
                  isEditing ? (
                    <div className="mt-3 space-y-3">
                      <select
                        value={editingClub}
                        onChange={(event) => setEditingClub(event.target.value)}
                        className="w-full rounded-xl border border-stone-300 px-3 py-3 text-sm focus:border-emerald-600"
                      >
                        {(DEFAULT_CLUBS.includes(editingClub as typeof DEFAULT_CLUBS[number])
                          ? DEFAULT_CLUBS
                          : [editingClub, ...DEFAULT_CLUBS]
                        ).map((club) => (
                          <option key={club} value={club}>
                            {club}
                          </option>
                        ))}
                      </select>
                      <div className="flex flex-wrap gap-2">
                        <button
                          type="button"
                          onClick={() => saveShot(shot.id)}
                          disabled={isBusy}
                          className="min-h-11 rounded-xl bg-emerald-700 px-4 py-2 text-sm font-semibold text-white transition hover:bg-emerald-800 disabled:opacity-60"
                        >
                          {isBusy ? 'Saving…' : 'Save'}
                        </button>
                        <button
                          type="button"
                          onClick={() => setEditingShotId(null)}
                          className="min-h-11 rounded-xl border border-stone-300 px-4 py-2 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
                        >
                          Cancel
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-3 flex flex-wrap gap-2">
                      <button
                        type="button"
                        onClick={() => startEditing(shot)}
                        disabled={isBusy}
                        className="min-h-11 rounded-xl border border-stone-300 px-4 py-2 text-sm font-semibold text-stone-700 transition hover:bg-stone-50 disabled:opacity-60"
                      >
                        Edit club
                      </button>
                      <button
                        type="button"
                        onClick={() => removeShot(shot.id)}
                        disabled={isBusy}
                        className="min-h-11 rounded-xl border border-red-200 px-4 py-2 text-sm font-semibold text-red-700 transition hover:bg-red-50 disabled:opacity-60"
                      >
                        {isBusy ? 'Removing…' : 'Delete shot'}
                      </button>
                    </div>
                  )
                ) : null}
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}
