import { NextResponse } from 'next/server'
import { ZodError } from 'zod'

import { createRound, listCompletedRounds } from '@/lib/rounds'

export async function GET() {
  const rounds = await listCompletedRounds()
  return NextResponse.json(rounds)
}

export async function POST(request: Request) {
  try {
    const body = await request.json()
    const round = await createRound(body.courseId, body.playMode)

    return NextResponse.json({ id: round.id }, { status: 201 })
  } catch (error) {
    if (error instanceof ZodError) {
      return NextResponse.json(
        {
          error: error.issues[0]?.message ?? 'Invalid round data',
        },
        { status: 400 },
      )
    }

    if (error instanceof Error) {
      const status =
        error.message === 'Course not found'
          ? 404
          : error.message === 'A round is already in progress'
            ? 409
            : 400

      return NextResponse.json({ error: error.message }, { status })
    }

    return NextResponse.json({ error: 'Unable to start round' }, { status: 500 })
  }
}
