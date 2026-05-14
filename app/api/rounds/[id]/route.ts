import { NextResponse } from 'next/server'

import { getRoundById } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function GET(_: Request, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid round id' }, { status: 400 })
  }

  const round = await getRoundById(id)

  if (!round) {
    return NextResponse.json({ error: 'Round not found' }, { status: 404 })
  }

  return NextResponse.json(round)
}
