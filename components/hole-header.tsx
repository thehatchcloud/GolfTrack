type HoleHeaderProps = {
  holeNumber: number
  par: number
  strokes: number
}

export function HoleHeader({ holeNumber, par, strokes }: HoleHeaderProps) {
  return (
    <section className="rounded-3xl bg-gradient-to-br from-emerald-700 to-emerald-900 p-5 text-white shadow-lg">
      <p className="text-sm font-medium uppercase tracking-[0.2em] text-emerald-100">Current hole</p>
      <div className="mt-3 flex items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">Hole {holeNumber}</h1>
          <p className="mt-1 text-sm text-emerald-50/90">Par {par}</p>
        </div>
        <div className="rounded-2xl bg-white/10 px-4 py-3 text-right backdrop-blur-sm">
          <p className="text-xs font-medium uppercase tracking-[0.2em] text-emerald-100">Strokes</p>
          <p className="mt-1 text-3xl font-semibold">{strokes}</p>
        </div>
      </div>
    </section>
  )
}
