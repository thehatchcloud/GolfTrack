import { NextResponse } from 'next/server'
import { ZodError } from 'zod'

export class NotFoundError extends Error {}
export class ConflictError extends Error {}
export class ValidationError extends Error {}

export function toResponse(error: unknown): NextResponse {
  if (error instanceof NotFoundError) {
    return NextResponse.json({ error: error.message }, { status: 404 })
  }
  if (error instanceof ConflictError) {
    return NextResponse.json({ error: error.message }, { status: 409 })
  }
  if (error instanceof ValidationError) {
    return NextResponse.json({ error: error.message }, { status: 400 })
  }
  if (error instanceof ZodError) {
    return NextResponse.json({ error: error.issues[0]?.message ?? 'Invalid input' }, { status: 400 })
  }
  console.error(error)
  return NextResponse.json({ error: 'Internal server error' }, { status: 500 })
}
