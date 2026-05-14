type RoundSummaryBarProps = {
  courseName: string
  currentHole: number
  holeCount: number
  totalStrokes: number
  playLabel?: string
  holePosition?: number
}

export function RoundSummaryBar({
  courseName,
  currentHole,
  holeCount,
  totalStrokes,
  playLabel,
  holePosition,
}: RoundSummaryBarProps) {
  return (
    <section className="rounded-2xl border border-stone-200 bg-white p-4 shadow-sm">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-sm font-semibold text-stone-900">{courseName}</p>
          <p className="text-sm text-stone-600">
            Hole {currentHole} of {holeCount}
            {playLabel ? ` · ${playLabel}` : ''}
          </p>
          {holePosition && holePosition !== currentHole ? (
            <p className="mt-1 text-xs text-stone-500">Position {holePosition} in this round</p>
          ) : null}
        </div>
        <div className="rounded-xl bg-stone-100 px-4 py-3 text-center">
          <p className="text-xs font-medium uppercase tracking-[0.2em] text-stone-500">Round strokes</p>
          <p className="mt-1 text-2xl font-semibold text-stone-900">{totalStrokes}</p>
        </div>
      </div>
    </section>
  )
}
