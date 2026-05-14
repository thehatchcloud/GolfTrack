import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { AppShell } from '@/components/app-shell'
import { getRoundPlayLabel } from '@/lib/round-play'
import { calculateRoundTotals, formatRelativeToPar } from '@/lib/scoring'
import { getRoundById } from '@/lib/rounds'

export const dynamic = 'force-dynamic'

export default async function RoundDetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = await params
  const round = await getRoundById(Number(id))

  if (!round) {
    notFound()
  }

  if (round.status === 'in_progress') {
    redirect(`/rounds/${round.id}/play`)
  }

  const totals = calculateRoundTotals(round.holes)

  return (
    <AppShell>
      <div className="space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-emerald-700">Completed Round</p>
            <h1 className="text-2xl font-semibold tracking-tight text-stone-900">{round.course.name}</h1>
            <p className="mt-1 text-sm text-stone-600">
              {getRoundPlayLabel(
                round.course.holeCount,
                round.holes.map((hole) => hole.holeNumber),
              )}
              {' · '}
              {round.finishedAt
                ? new Intl.DateTimeFormat('en-US', {
                    dateStyle: 'medium',
                    timeStyle: 'short',
                  }).format(round.finishedAt)
                : 'Completed round'}
            </p>
          </div>
          <Link
            href="/rounds"
            className="rounded-xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
          >
            Back to Rounds
          </Link>
        </div>

        <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
          <div className="grid grid-cols-3 gap-3 text-center">
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
        </section>

        <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
          <h2 className="mb-4 text-lg font-semibold text-stone-900">Hole-by-hole</h2>
          <div className="space-y-4">
            {round.holes.map((hole) => (
              <div key={hole.id} className="rounded-2xl border border-stone-200 p-4">
                <div className="mb-3 flex items-center justify-between gap-4">
                  <div>
                    <p className="text-base font-semibold text-stone-900">Hole {hole.holeNumber}</p>
                    <p className="text-sm text-stone-600">Par {hole.par}</p>
                  </div>
                  <div className="text-right">
                    <p className="text-lg font-semibold text-stone-900">{hole.strokes}</p>
                    <p className="text-sm text-stone-600">{formatRelativeToPar(hole.strokes - hole.par)}</p>
                  </div>
                </div>

                {hole.shots.length === 0 ? (
                  <p className="text-sm text-stone-500">No shots recorded.</p>
                ) : (
                  <div className="flex flex-wrap gap-2">
                    {hole.shots.map((shot) => (
                      <span
                        key={shot.id}
                        className="rounded-full bg-emerald-50 px-3 py-1 text-sm font-medium text-emerald-800"
                      >
                        {shot.shotNumber}. {shot.club}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>

        <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
          <h2 className="text-lg font-semibold text-stone-900">Round notes</h2>
          <p className="mt-3 whitespace-pre-wrap text-sm leading-6 text-stone-700">
            {round.note?.trim() ? round.note : 'No notes saved for this round.'}
          </p>
        </section>
      </div>
    </AppShell>
  )
}
