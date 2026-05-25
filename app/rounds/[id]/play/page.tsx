import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { auth } from '@/auth'
import { AppShell } from '@/components/app-shell'
import { CancelRoundButton } from '@/components/cancel-round-button'
import { HoleProgressStrip } from '@/components/hole-progress-strip'
import { LiveRoundControls } from '@/components/live-round-controls'
import { RoundSummaryBar } from '@/components/round-summary-bar'
import { getCurrentHolePosition, getRoundPlayLabel } from '@/lib/round-play'
import { calculateRoundTotals } from '@/lib/scoring'
import { getRoundById } from '@/lib/rounds'

export const dynamic = 'force-dynamic'

export default async function PlayRoundPage({
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

  const holeNumbers = round.holes.map((hole) => hole.holeNumber)
  const currentHoleData =
    round.holes.find((hole) => hole.holeNumber === round.currentHole) ?? round.holes[0]

  const totals = calculateRoundTotals(round.holes)
  const playLabel = getRoundPlayLabel(round.course.holeCount, holeNumbers)
  const holePosition = getCurrentHolePosition(holeNumbers, currentHoleData.holeNumber)

  return (
    <AppShell>
      <div className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p className="text-sm font-medium text-emerald-700">Live Round</p>
            <h1 className="text-2xl font-semibold tracking-tight text-stone-900">{round.course.name}</h1>
            <p className="mt-1 text-sm text-stone-600">Simple thumb-friendly shot entry while you play.</p>
          </div>
          <div className="hidden items-center gap-3 sm:flex">
            <CancelRoundButton roundId={round.id} redirectTo="/" label="Cancel Round" />
            <Link
              href={`/rounds/${round.id}/review`}
              className="rounded-xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
            >
              Review Round
            </Link>
          </div>
        </div>

        <RoundSummaryBar
          courseName={round.course.name}
          currentHole={currentHoleData.holeNumber}
          holeCount={round.holes.length}
          totalStrokes={totals.totalStrokes}
          playLabel={playLabel}
          holePosition={holePosition}
        />

        <HoleProgressStrip holes={round.holes} currentHole={currentHoleData.holeNumber} />

        <LiveRoundControls
          roundId={round.id}
          currentHole={currentHoleData.holeNumber}
          holeNumbers={holeNumbers}
          currentHoleData={currentHoleData}
        />
      </div>
    </AppShell>
  )
}
