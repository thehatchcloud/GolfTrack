import { NextResponse } from 'next/server'

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
    if (error instanceof Error) {
      const status = error.message === 'Round not found' ? 404 : 400
      return NextResponse.json({ error: error.message }, { status })
    }

    return NextResponse.json({ error: 'Unable to cancel round' }, { status: 500 })
  }
}
