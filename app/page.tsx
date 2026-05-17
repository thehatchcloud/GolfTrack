import Link from 'next/link'

import { AppShell } from '@/components/app-shell'
import { SectionCard } from '@/components/section-card'
import { getInProgressRound, listCompletedRounds } from '@/lib/rounds'
import { getRoundPlayLabel } from '@/lib/round-play'
import { formatRelativeToPar } from '@/lib/scoring'

export const dynamic = 'force-dynamic'

export default async function HomePage() {
  const [inProgressRound, recentRounds] = await Promise.all([
    getInProgressRound(),
    listCompletedRounds(3),
  ])

  return (
    <AppShell>
      <section className="mb-6 rounded-3xl bg-gradient-to-br from-emerald-700 to-emerald-900 p-6 text-white shadow-lg">
        <p className="text-sm font-medium uppercase tracking-[0.2em] text-emerald-100">Golf Score Tracker</p>
        <h1 className="mt-3 text-3xl font-semibold tracking-tight">Track your round, one shot at a time.</h1>
        <p className="mt-3 max-w-2xl text-sm leading-6 text-emerald-50/90">
          Mobile-first score tracking for simple course setup, quick live shot entry, and end-of-round review.
        </p>
        <div className="mt-5 flex flex-wrap gap-3">
          <Link
            href="/rounds/new"
            className="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-emerald-900 transition hover:bg-emerald-50"
            style={{ color: '#065f46' }}
          >
            Start New Round
          </Link>
          <Link
            href="/courses"
            className="rounded-xl border border-white/20 px-4 py-3 text-sm font-semibold text-white transition hover:bg-white/10"
          >
            Manage Courses
          </Link>
        </div>
      </section>

      <div className="grid gap-4 md:grid-cols-2">
        <SectionCard
          title="Continue round"
          description="Pick up where you left off if a round is still in progress."
        >
          {inProgressRound ? (
            <div className="space-y-3">
              <div>
                <p className="text-base font-semibold text-stone-900">{inProgressRound.course.name}</p>
                <p className="text-sm text-stone-600">
                  {getRoundPlayLabel(
                    inProgressRound.course.holeCount,
                    inProgressRound.holes.map((hole) => hole.holeNumber),
                  )}
                  {' · '}Currently on hole {inProgressRound.currentHole}
                </p>
              </div>
              <Link
                href={`/rounds/${inProgressRound.id}/play`}
                className="inline-flex rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white transition hover:bg-emerald-800"
              >
                Continue Round
              </Link>
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-sm text-stone-600">No round is currently in progress.</p>
              <Link
                href="/rounds/new"
                className="inline-flex rounded-xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
              >
                Start a round
              </Link>
            </div>
          )}
        </SectionCard>

        <SectionCard title="Recent rounds" description="Your latest completed rounds.">
          {recentRounds.length === 0 ? (
            <p className="text-sm text-stone-600">No completed rounds yet.</p>
          ) : (
            <div className="space-y-3">
              {recentRounds.map((round) => (
                <Link
                  key={round.id}
                  href={`/rounds/${round.id}`}
                  className="block rounded-xl border border-stone-200 px-4 py-3 transition hover:border-emerald-300 hover:bg-emerald-50/40"
                >
                  <p className="text-sm font-semibold text-stone-900">{round.course.name}</p>
                  <p className="mt-1 text-sm text-stone-600">
                    {round.totalStrokes} strokes · {formatRelativeToPar(round.relativeToPar)}
                  </p>
                </Link>
              ))}
            </div>
          )}
        </SectionCard>
      </div>
    </AppShell>
  )
}
