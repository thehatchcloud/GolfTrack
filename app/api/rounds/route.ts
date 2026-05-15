import { NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
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
    return toResponse(error)
  }
}
