import { NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { undoLastShot } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function POST(_: Request, context: { params: Promise<{ id: string; holeNumber: string }> }) {
  const params = await context.params
  const roundId = parseId(params.id)
  const holeNumber = parseId(params.holeNumber)

  if (Number.isNaN(roundId) || Number.isNaN(holeNumber)) {
    return NextResponse.json({ error: 'Invalid round or hole id' }, { status: 400 })
  }

  try {
    const roundHole = await undoLastShot(roundId, holeNumber)
    return NextResponse.json(roundHole)
  } catch (error) {
    return toResponse(error)
  }
}
