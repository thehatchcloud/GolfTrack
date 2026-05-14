import { NextResponse } from 'next/server'
import { ZodError } from 'zod'

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
    if (error instanceof ZodError) {
      return NextResponse.json(
        {
          error: error.issues[0]?.message ?? 'Invalid shot data',
        },
        { status: 400 },
      )
    }

    if (error instanceof Error) {
      const status = error.message === 'Shot not found' || error.message === 'Round hole not found' ? 404 : 400
      return NextResponse.json({ error: error.message }, { status })
    }

    return NextResponse.json({ error: 'Unable to update shot' }, { status: 500 })
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
    if (error instanceof Error) {
      const status = error.message === 'Shot not found' || error.message === 'Round hole not found' ? 404 : 400
      return NextResponse.json({ error: error.message }, { status })
    }

    return NextResponse.json({ error: 'Unable to delete shot' }, { status: 500 })
  }
}
