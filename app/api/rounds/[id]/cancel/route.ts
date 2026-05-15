import { NextResponse } from 'next/server'

import { toResponse } from '@/lib/errors'
import { cancelRound } from '@/lib/rounds'

function parseId(value: string) {
  return Number.parseInt(value, 10)
}

export async function POST(_: Request, context: { params: Promise<{ id: string }> }) {
  const params = await context.params
  const id = parseId(params.id)

  if (Number.isNaN(id)) {
    return NextResponse.json({ error: 'Invalid round id' }, { status: 400 })
  }

  try {
    const result = await cancelRound(id)
    return NextResponse.json(result)
  } catch (error) {
    return toResponse(error)
  }
}
