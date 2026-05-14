import Link from 'next/link'

import { AppShell } from '@/components/app-shell'
import { SectionCard } from '@/components/section-card'
import { getRoundPlayLabel } from '@/lib/round-play'
import { formatRelativeToPar } from '@/lib/scoring'
import { listCompletedRounds } from '@/lib/rounds'

export const dynamic = 'force-dynamic'

export default async function RoundsPage() {
  const rounds = await listCompletedRounds()

  return (
    <AppShell>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-stone-900">Past Rounds</h1>
          <p className="mt-1 text-sm text-stone-600">Review completed rounds and notes.</p>
        </div>
        <Link
          href="/rounds/new"
          className="rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white shadow-sm transition hover:bg-emerald-800"
        >
          New Round
        </Link>
      </div>

      {rounds.length === 0 ? (
        <SectionCard
          title="No completed rounds"
          description="Play and submit a round to see it here."
        >
          <Link
            href="/rounds/new"
            className="inline-flex rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white transition hover:bg-emerald-800"
          >
            Start a round
          </Link>
        </SectionCard>
      ) : (
        <div className="grid gap-4">
          {rounds.map((round) => (
            <Link
              key={round.id}
              href={`/rounds/${round.id}`}
              className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm transition hover:border-emerald-300 hover:shadow"
            >
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h2 className="text-lg font-semibold text-stone-900">{round.course.name}</h2>
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
                <div className="text-right">
                  <p className="text-lg font-semibold text-stone-900">{round.totalStrokes}</p>
                  <p className="text-sm text-stone-600">{formatRelativeToPar(round.relativeToPar)}</p>
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
    </AppShell>
  )
}
