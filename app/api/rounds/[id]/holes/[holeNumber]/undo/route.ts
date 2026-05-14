import { NextResponse } from 'next/server'

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
    if (error instanceof Error) {
      const status = error.message === 'Round hole not found' ? 404 : 400
      return NextResponse.json({ error: error.message }, { status })
    }

    return NextResponse.json({ error: 'Unable to undo shot' }, { status: 500 })
  }
}
