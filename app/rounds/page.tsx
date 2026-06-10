import Link from 'next/link'

import { auth } from '@/auth'
import { AppShell } from '@/components/app-shell'
import { SectionCard } from '@/components/section-card'
import { getRoundPlayLabelFromMode } from '@/lib/round-play'
import { formatRelativeToPar } from '@/lib/scoring'
import { getInProgressRound, listCompletedRounds } from '@/lib/rounds'

export const dynamic = 'force-dynamic'

const PAGE_SIZE = 20

export default async function RoundsPage({
  searchParams,
}: {
  searchParams: Promise<{ page?: string }>
}) {
  const session = await auth()
  const userId = session!.user!.id
  const params = await searchParams
  const page = Math.max(0, parseInt(params.page ?? '0', 10) || 0)

  const [inProgressRound, rounds] = await Promise.all([
    getInProgressRound(userId),
    listCompletedRounds(userId, PAGE_SIZE + 1, page),
  ])

  const hasMore = rounds.length > PAGE_SIZE
  const displayRounds = hasMore ? rounds.slice(0, PAGE_SIZE) : rounds

  return (
    <AppShell>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-stone-900">Rounds</h1>
          <p className="mt-1 text-sm text-stone-600">Review completed rounds and notes.</p>
        </div>
        <Link
          href="/rounds/new"
          className="rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white shadow-sm transition hover:bg-emerald-800"
        >
          New Round
        </Link>
      </div>

      {inProgressRound && (
        <Link
          href={`/rounds/${inProgressRound.id}/play`}
          className="mb-6 flex items-center justify-between rounded-2xl border border-emerald-300 bg-emerald-50 p-5 shadow-sm transition hover:border-emerald-400 hover:bg-emerald-100/60"
        >
          <div>
            <div className="mb-1 flex items-center gap-2">
              <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-600 px-2.5 py-0.5 text-xs font-semibold text-white">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                In Progress
              </span>
            </div>
            <h2 className="text-base font-semibold text-stone-900">{inProgressRound.course.name}</h2>
            <p className="mt-0.5 text-sm text-stone-600">
              {getRoundPlayLabelFromMode(
                inProgressRound.playMode,
                inProgressRound.course.holeCount,
              )}
              {' · '}Hole {inProgressRound.currentHole}
            </p>
          </div>
          <span className="shrink-0 rounded-xl bg-emerald-700 px-4 py-2.5 text-sm font-semibold text-white">
            Continue
          </span>
        </Link>
      )}

      {displayRounds.length === 0 ? (
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
          {displayRounds.map((round) => (
            <Link
              key={round.id}
              href={`/rounds/${round.id}`}
              className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm transition hover:border-emerald-300 hover:shadow"
            >
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h2 className="text-lg font-semibold text-stone-900">{round.course.name}</h2>
                  <p className="mt-1 text-sm text-stone-600">
                    {getRoundPlayLabelFromMode(round.playMode, round.course.holeCount)}
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
                  <p className="text-lg font-semibold text-stone-900">{round.totalStrokes ?? 0}</p>
                  <p className="text-sm text-stone-600">{formatRelativeToPar(round.relativeToPar ?? 0)}</p>
                </div>
              </div>
            </Link>
          ))}

          {(page > 0 || hasMore) && (
            <div className="flex items-center justify-between pt-2">
              {page > 0 ? (
                <Link
                  href={page === 1 ? '/rounds' : `/rounds?page=${page - 1}`}
                  className="rounded-xl border border-stone-300 px-4 py-2.5 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
                >
                  ← Newer
                </Link>
              ) : (
                <span />
              )}
              {hasMore && (
                <Link
                  href={`/rounds?page=${page + 1}`}
                  className="rounded-xl border border-stone-300 px-4 py-2.5 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
                >
                  Older →
                </Link>
              )}
            </div>
          )}
        </div>
      )}
    </AppShell>
  )
}
