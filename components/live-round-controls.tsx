'use client'

import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useOptimistic, useState, useTransition } from 'react'

import { CancelRoundButton } from '@/components/cancel-round-button'
import { ClubPad } from '@/components/club-pad'
import { HoleHeader } from '@/components/hole-header'
import { ShotList } from '@/components/shot-list'

type Shot = {
  id: number
  shotNumber: number
  club: string
}

type Hole = {
  holeNumber: number
  par: number
  strokes: number
  shots: Shot[]
}

type OptimisticAction = { type: 'add'; club: string } | { type: 'undo' }

type LiveRoundControlsProps = {
  roundId: number
  currentHole: number
  holeNumbers: number[]
  currentHoleData: Hole
}

export function LiveRoundControls({
  roundId,
  currentHole,
  holeNumbers,
  currentHoleData,
}: LiveRoundControlsProps) {
  const router = useRouter()
  const [error, setError] = useState<string | null>(null)
  const [isMoveLoading, setIsMoveLoading] = useState(false)
  const [isPending, startTransition] = useTransition()

  const [optimisticHole, addOptimistic] = useOptimistic(
    currentHoleData,
    (state: Hole, action: OptimisticAction) => {
      if (action.type === 'add') {
        return {
          ...state,
          strokes: state.strokes + 1,
          shots: [
            ...state.shots,
            { id: -Date.now(), shotNumber: state.shots.length + 1, club: action.club },
          ],
        }
      }
      if (action.type === 'undo') {
        return {
          ...state,
          strokes: Math.max(0, state.strokes - 1),
          shots: state.shots.slice(0, -1),
        }
      }
      return state
    }
  )

  const currentIndex = holeNumbers.indexOf(currentHole)
  const previousHole = currentIndex > 0 ? holeNumbers[currentIndex - 1] : null
  const nextHole =
    currentIndex >= 0 && currentIndex < holeNumbers.length - 1 ? holeNumbers[currentIndex + 1] : null

  function addShot(club: string) {
    setError(null)
    startTransition(async () => {
      addOptimistic({ type: 'add', club })

      try {
        const response = await fetch(
          `/api/rounds/${roundId}/holes/${currentHoleData.holeNumber}/shots`,
          {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ club }),
          }
        )

        if (!response.ok) {
          const body = (await response.json().catch(() => null)) as { error?: string } | null
          throw new Error(body?.error ?? 'Unable to add shot')
        }

        router.refresh()
      } catch (addError) {
        setError(addError instanceof Error ? addError.message : 'Unable to add shot')
      }
    })
  }

  function undoShot() {
    setError(null)
    startTransition(async () => {
      addOptimistic({ type: 'undo' })

      try {
        const response = await fetch(
          `/api/rounds/${roundId}/holes/${currentHoleData.holeNumber}/undo`,
          { method: 'POST' }
        )

        if (!response.ok) {
          const body = (await response.json().catch(() => null)) as { error?: string } | null
          throw new Error(body?.error ?? 'Unable to undo shot')
        }

        router.refresh()
      } catch (undoError) {
        setError(undoError instanceof Error ? undoError.message : 'Unable to undo shot')
      }
    })
  }

  async function moveToHole(targetHole: number | null) {
    if (!targetHole || targetHole === currentHole) return

    setError(null)
    setIsMoveLoading(true)

    try {
      const response = await fetch(`/api/rounds/${roundId}/current-hole`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ currentHole: targetHole }),
      })

      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(body?.error ?? 'Unable to update current hole')
      }

      router.push(`/rounds/${roundId}/play`)
      router.refresh()
    } catch (moveError) {
      setError(moveError instanceof Error ? moveError.message : 'Unable to update current hole')
    } finally {
      setIsMoveLoading(false)
    }
  }

  const isBusy = isPending || isMoveLoading

  return (
    <div className="space-y-4 pb-40 sm:pb-36">
      <HoleHeader
        holeNumber={optimisticHole.holeNumber}
        par={optimisticHole.par}
        strokes={optimisticHole.strokes}
      />

      <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold text-stone-900">Record a shot</h2>
            <p className="mt-1 text-sm text-stone-600">Tap the club you used. Each tap adds one stroke.</p>
          </div>
          <div className="rounded-full bg-stone-100 px-3 py-1 text-xs font-medium text-stone-700">
            {isBusy ? 'Updating…' : 'Ready'}
          </div>
        </div>
        <ClubPad onSelectClub={addShot} disabled={isBusy} />
      </section>

      <ShotList
        shots={optimisticHole.shots}
        roundId={roundId}
        holeNumber={currentHoleData.holeNumber}
        editable
      />

      {error ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm font-medium text-red-700">
          {error}
        </div>
      ) : null}

      <div className="fixed inset-x-0 bottom-0 z-20 border-t border-stone-200 bg-white/95 px-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] pt-3 shadow-[0_-8px_24px_rgba(0,0,0,0.08)] backdrop-blur">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-3 sm:flex-row">
          <div className="grid grid-cols-3 gap-3 sm:flex sm:flex-1">
            <button
              type="button"
              onClick={() => moveToHole(previousHole)}
              disabled={!previousHole || isBusy}
              className="min-h-12 rounded-2xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Prev
            </button>
            <button
              type="button"
              onClick={undoShot}
              disabled={optimisticHole.shots.length === 0 || isBusy}
              className="min-h-12 rounded-2xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Undo
            </button>
            <button
              type="button"
              onClick={() => moveToHole(nextHole)}
              disabled={!nextHole || isBusy}
              className="min-h-12 rounded-2xl bg-emerald-700 px-5 py-3 text-sm font-semibold text-white transition hover:bg-emerald-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Next
            </button>
          </div>

          <div className="grid grid-cols-2 gap-3 sm:flex sm:gap-3">
            <CancelRoundButton roundId={roundId} redirectTo="/" label="Cancel" />
            <Link
              href={`/rounds/${roundId}/review`}
              className="flex min-h-12 items-center justify-center rounded-2xl border border-stone-300 px-5 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
            >
              Review Round
            </Link>
          </div>
        </div>
      </div>
    </div>
  )
}
