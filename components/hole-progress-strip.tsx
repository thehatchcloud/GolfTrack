type HoleProgressStripProps = {
  holes: Array<{
    id: number
    holeNumber: number
    strokes: number
  }>
  currentHole: number
}

export function HoleProgressStrip({ holes, currentHole }: HoleProgressStripProps) {
  return (
    <section className="rounded-2xl border border-stone-200 bg-white p-4 shadow-sm">
      <div className="mb-3 flex items-center justify-between gap-4">
        <h2 className="text-sm font-semibold uppercase tracking-[0.2em] text-stone-500">Round progress</h2>
        <p className="text-sm text-stone-600">Current hole highlighted</p>
      </div>
      <div className="flex gap-2 overflow-x-auto pb-1">
        {holes.map((hole) => {
          const isCurrent = hole.holeNumber === currentHole
          const isPlayed = hole.strokes > 0

          return (
            <div
              key={hole.id}
              className={`flex min-w-14 flex-col items-center rounded-2xl border px-3 py-2 text-center transition ${
                isCurrent
                  ? 'border-emerald-700 bg-emerald-700 text-white'
                  : isPlayed
                    ? 'border-emerald-200 bg-emerald-50 text-emerald-900'
                    : 'border-stone-200 bg-stone-50 text-stone-700'
              }`}
            >
              <span className="text-xs font-medium uppercase tracking-[0.15em] opacity-80">Hole</span>
              <span className="mt-1 text-base font-semibold">{hole.holeNumber}</span>
              <span className="mt-1 text-xs font-medium opacity-80">{hole.strokes} st</span>
            </div>
          )
        })}
      </div>
    </section>
  )
}
