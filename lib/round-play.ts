export type RoundPlayMode = 'full' | 'front9' | 'back9'

export function getRoundPlayMode(courseHoleCount: number, holeNumbers: number[]): RoundPlayMode {
  if (courseHoleCount === 18 && holeNumbers.length === 9) {
    if (holeNumbers[0] === 1 && holeNumbers.at(-1) === 9) {
      return 'front9'
    }

    if (holeNumbers[0] === 10 && holeNumbers.at(-1) === 18) {
      return 'back9'
    }
  }

  return 'full'
}

export function getRoundPlayLabel(courseHoleCount: number, holeNumbers: number[]) {
  const mode = getRoundPlayMode(courseHoleCount, holeNumbers)

  if (mode === 'front9') return 'Front 9'
  if (mode === 'back9') return 'Back 9'
  return courseHoleCount === 9 ? '9 Holes' : 'Full Course'
}

export function getCurrentHolePosition(holeNumbers: number[], currentHole: number) {
  const index = holeNumbers.indexOf(currentHole)
  return index === -1 ? 1 : index + 1
}
