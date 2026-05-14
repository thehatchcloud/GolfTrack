import { NextResponse } from 'next/server'

import { getInProgressRound } from '@/lib/rounds'

export async function GET() {
  const round = await getInProgressRound()
  return NextResponse.json(round)
}
