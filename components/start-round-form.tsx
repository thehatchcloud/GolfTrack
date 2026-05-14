'use client'

import { useMemo, useState } from 'react'
import { useRouter } from 'next/navigation'

type CourseOption = {
  id: number
  name: string
  holeCount: number
  totalPar: number
}

type PlayMode = 'full' | 'front9' | 'back9'

export function StartRoundForm({ courses }: { courses: CourseOption[] }) {
  const router = useRouter()
  const [courseId, setCourseId] = useState<number | null>(courses[0]?.id ?? null)
  const [playMode, setPlayMode] = useState<PlayMode>('full')
  const [error, setError] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)

  const selectedCourse = useMemo(
    () => courses.find((course) => course.id === courseId) ?? null,
    [courseId, courses],
  )

  const supportsNineHoleChoice = selectedCourse?.holeCount === 18

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()

    if (!courseId) {
      setError('Select a course to start a round')
      return
    }

    setError(null)
    setIsSubmitting(true)

    try {
      const response = await fetch('/api/rounds', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          courseId,
          playMode: supportsNineHoleChoice ? playMode : 'full',
        }),
      })

      const body = (await response.json().catch(() => null)) as { id?: number; error?: string } | null

      if (!response.ok || !body?.id) {
        throw new Error(body?.error ?? 'Unable to start round')
      }

      router.push(`/rounds/${body.id}/play`)
      router.refresh()
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : 'Unable to start round')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight text-stone-900">Start a new round</h1>
        <p className="text-sm text-stone-600">Choose a course and jump right into live scoring.</p>
      </div>

      <div className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
        <div className="space-y-3">
          {courses.map((course) => {
            const selected = courseId === course.id

            return (
              <label
                key={course.id}
                className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-4 transition ${
                  selected
                    ? 'border-emerald-600 bg-emerald-50'
                    : 'border-stone-200 bg-white hover:border-stone-300 hover:bg-stone-50'
                }`}
              >
                <input
                  type="radio"
                  name="courseId"
                  checked={selected}
                  onChange={() => {
                    setCourseId(course.id)
                    setPlayMode('full')
                  }}
                  className="mt-1 h-4 w-4 accent-emerald-700"
                />
                <div>
                  <p className="text-base font-semibold text-stone-900">{course.name}</p>
                  <p className="mt-1 text-sm text-stone-600">
                    {course.holeCount} holes · Total par {course.totalPar}
                  </p>
                </div>
              </label>
            )
          })}
        </div>
      </div>

      {supportsNineHoleChoice ? (
        <div className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
          <div className="mb-4">
            <h2 className="text-lg font-semibold text-stone-900">Round length</h2>
            <p className="mt-1 text-sm text-stone-600">For 18-hole courses, choose the full course, front 9, or back 9.</p>
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            {[
              { value: 'full', label: 'Full Course', detail: 'Play all 18 holes' },
              { value: 'front9', label: 'Front 9', detail: 'Play holes 1–9' },
              { value: 'back9', label: 'Back 9', detail: 'Play holes 10–18' },
            ].map((option) => {
              const selected = playMode === option.value

              return (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => setPlayMode(option.value as PlayMode)}
                  className={`rounded-2xl border px-4 py-4 text-left transition ${
                    selected
                      ? 'border-emerald-600 bg-emerald-50 text-emerald-900'
                      : 'border-stone-200 bg-white text-stone-700 hover:border-stone-300 hover:bg-stone-50'
                  }`}
                >
                  <p className="text-base font-semibold">{option.label}</p>
                  <p className="mt-1 text-sm opacity-80">{option.detail}</p>
                </button>
              )
            })}
          </div>
        </div>
      ) : null}

      {error ? <p className="text-sm font-medium text-red-600">{error}</p> : null}

      <div className="sticky bottom-4 rounded-2xl border border-stone-200 bg-white/95 p-3 shadow-lg backdrop-blur">
        <button
          type="submit"
          disabled={isSubmitting || courses.length === 0}
          className="min-h-[56px] w-full rounded-xl bg-emerald-700 px-4 py-3 text-base font-semibold text-white transition hover:bg-emerald-800 active:scale-[0.99] disabled:cursor-not-allowed disabled:opacity-60"
        >
          {isSubmitting ? 'Starting…' : 'Start Round'}
        </button>
      </div>
    </form>
  )
}
