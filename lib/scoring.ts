export function calculateRelativeToPar(strokes: number, par: number) {
  return strokes - par
}

export function calculateRoundTotals<T extends { par: number; strokes: number }>(holes: T[]) {
  const totalPar = holes.reduce((sum, hole) => sum + hole.par, 0)
  const totalStrokes = holes.reduce((sum, hole) => sum + hole.strokes, 0)
  const relativeToPar = totalStrokes - totalPar

  return {
    totalPar,
    totalStrokes,
    relativeToPar,
  }
}

export function formatRelativeToPar(value: number) {
  if (value === 0) return 'E'
  return value > 0 ? `+${value}` : `${value}`
}
