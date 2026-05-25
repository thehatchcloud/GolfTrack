import { notFound, redirect } from 'next/navigation'

import { auth } from '@/auth'
import { AppShell } from '@/components/app-shell'
import { CancelRoundButton } from '@/components/cancel-round-button'
import { ReviewRoundForm } from '@/components/review-round-form'
import { calculateRoundTotals, formatRelativeToPar } from '@/lib/scoring'
import { getRoundById } from '@/lib/rounds'

export const dynamic = 'force-dynamic'

export default async function ReviewRoundPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const session = await auth()
  const userId = session!.user!.id
  const { id } = await params
  const round = await getRoundById(Number(id), userId)

  if (!round) {
    notFound()
  }

  if (round.status !== 'in_progress') {
    redirect(`/rounds/${round.id}`)
  }

  const totals = calculateRoundTotals(round.holes)

  return (
    <AppShell>
      <div className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <p className="text-sm font-medium text-emerald-700">Review Round</p>
            <h1 className="text-2xl font-semibold tracking-tight text-stone-900">{round.course.name}</h1>
            <p className="mt-1 text-sm text-stone-600">Check your hole scores before you submit the round.</p>
          </div>
          <CancelRoundButton roundId={round.id} redirectTo="/" label="Cancel Round" />
        </div>

        <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
          <div className="mb-4 grid grid-cols-3 gap-3 text-center">
            <div className="rounded-xl bg-stone-100 px-4 py-3">
              <p className="text-xs font-medium uppercase tracking-[0.2em] text-stone-500">Par</p>
              <p className="mt-1 text-2xl font-semibold text-stone-900">{totals.totalPar}</p>
            </div>
            <div className="rounded-xl bg-stone-100 px-4 py-3">
              <p className="text-xs font-medium uppercase tracking-[0.2em] text-stone-500">Strokes</p>
              <p className="mt-1 text-2xl font-semibold text-stone-900">{totals.totalStrokes}</p>
            </div>
            <div className="rounded-xl bg-stone-100 px-4 py-3">
              <p className="text-xs font-medium uppercase tracking-[0.2em] text-stone-500">To Par</p>
              <p className="mt-1 text-2xl font-semibold text-stone-900">{formatRelativeToPar(totals.relativeToPar)}</p>
            </div>
          </div>

          <div className="space-y-2">
            {round.holes.map((hole) => (
              <div
                key={hole.id}
                className="flex items-center justify-between rounded-xl border border-stone-200 px-4 py-3"
              >
                <div>
                  <p className="text-sm font-semibold text-stone-900">Hole {hole.holeNumber}</p>
                  <p className="text-sm text-stone-600">Par {hole.par}</p>
                </div>
                <div className="text-right">
                  <p className="text-lg font-semibold text-stone-900">{hole.strokes}</p>
                  <p className="text-sm text-stone-600">{formatRelativeToPar(hole.strokes - hole.par)}</p>
                </div>
              </div>
            ))}
          </div>
        </section>

        <ReviewRoundForm roundId={round.id} initialNote={round.note} />
      </div>
    </AppShell>
  )
}
