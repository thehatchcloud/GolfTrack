import { NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { deleteShot, updateShot } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function PATCH(
  request: Request,
  context: { params: Promise<{ id: string; holeNumber: string; shotId: string }> },
) {
  const params = await context.params
  const roundId = parseId(params.id)
  const holeNumber = parseId(params.holeNumber)
  const shotId = parseId(params.shotId)

  if (Number.isNaN(roundId) || Number.isNaN(holeNumber) || Number.isNaN(shotId)) {
    return NextResponse.json({ error: 'Invalid round, hole, or shot id' }, { status: 400 })
  }

  try {
    const body = await request.json()
    const shot = await updateShot(roundId, holeNumber, shotId, body.club)

    return NextResponse.json(shot)
  } catch (error) {
    return toResponse(error)
  }
}

export async function DELETE(
  _: Request,
  context: { params: Promise<{ id: string; holeNumber: string; shotId: string }> },
) {
  const params = await context.params
  const roundId = parseId(params.id)
  const holeNumber = parseId(params.holeNumber)
  const shotId = parseId(params.shotId)

  if (Number.isNaN(roundId) || Number.isNaN(holeNumber) || Number.isNaN(shotId)) {
    return NextResponse.json({ error: 'Invalid round, hole, or shot id' }, { status: 400 })
  }

  try {
    const roundHole = await deleteShot(roundId, holeNumber, shotId)
    return NextResponse.json(roundHole)
  } catch (error) {
    return toResponse(error)
  }
}
