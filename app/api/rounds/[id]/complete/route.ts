import { NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { completeRound } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function POST(request: Request, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid round id' }, { status: 400 })
  }

  try {
    const body = await request.json().catch(() => ({}))
    const round = await completeRound(id, body.note)

    return NextResponse.json({ id: round.id })
  } catch (error) {
    return toResponse(error)
  }
}
