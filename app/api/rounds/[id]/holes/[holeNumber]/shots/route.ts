import { NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { addShot } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function POST(
  request: Request,
  context: { params: Promise<{ id: string; holeNumber: string }> },
) {
  const params = await context.params
  const roundId = parseId(params.id)
  const holeNumber = parseId(params.holeNumber)

  if (Number.isNaN(roundId) || Number.isNaN(holeNumber)) {
    return NextResponse.json({ error: 'Invalid round or hole id' }, { status: 400 })
  }

  try {
    const body = await request.json()
    const roundHole = await addShot(roundId, holeNumber, body.club)

    return NextResponse.json(roundHole)
  } catch (error) {
    return toResponse(error)
  }
}
