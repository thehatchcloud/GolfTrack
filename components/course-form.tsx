'use client'

import { useMemo, useState } from 'react'
import { useRouter } from 'next/navigation'

type HoleInput = {
  holeNumber: number
  par: number
}

type CourseFormProps = {
  mode: 'create' | 'edit'
  initialValues?: {
    name: string
    holeCount: number
    holes: HoleInput[]
  }
  courseId?: number
}

function buildHoles(holeCount: number, existing?: HoleInput[]) {
  return Array.from({ length: holeCount }, (_, index) => {
    const holeNumber = index + 1
    const existingHole = existing?.find((hole) => hole.holeNumber === holeNumber)

    return {
      holeNumber,
      par: existingHole?.par ?? 4,
    }
  })
}

export function CourseForm({ mode, initialValues, courseId }: CourseFormProps) {
  const router = useRouter()

  const initialHoleCount = initialValues?.holeCount === 9 ? 9 : 18
  const [name, setName] = useState(initialValues?.name ?? '')
  const [holeCount, setHoleCount] = useState<9 | 18>(initialHoleCount as 9 | 18)
  const [holes, setHoles] = useState<HoleInput[]>(buildHoles(initialHoleCount, initialValues?.holes))
  const [error, setError] = useState<string | null>(null)
  const [isSaving, setIsSaving] = useState(false)

  const title = useMemo(() => (mode === 'create' ? 'Add Course' : 'Edit Course'), [mode])

  function updateHoleCount(nextHoleCount: 9 | 18) {
    setHoleCount(nextHoleCount)
    setHoles(buildHoles(nextHoleCount, holes))
  }

  function updatePar(holeNumber: number, par: number) {
    setHoles((current) =>
      current.map((hole) => (hole.holeNumber === holeNumber ? { ...hole, par } : hole)),
    )
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setIsSaving(true)
    setError(null)

    const payload = {
      name,
      holeCount,
      holes,
    }

    const endpoint = mode === 'create' ? '/api/courses' : `/api/courses/${courseId}`
    const method = mode === 'create' ? 'POST' : 'PUT'

    try {
      const response = await fetch(endpoint, {
        method,
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(body?.error ?? 'Unable to save course')
      }

      const body = (await response.json()) as { id?: number }
      router.push(mode === 'create' ? `/courses/${body.id}` : `/courses/${courseId}`)
      router.refresh()
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : 'Unable to save course')
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight text-stone-900">{title}</h1>
        <p className="text-sm text-stone-600">Set the course name and par for each hole.</p>
      </div>

      <div className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
        <div className="space-y-4">
          <label className="block space-y-2">
            <span className="text-sm font-medium text-stone-700">Course name</span>
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              className="w-full rounded-xl border border-stone-300 px-4 py-3 text-base outline-none ring-0 placeholder:text-stone-400 focus:border-emerald-600"
              placeholder="e.g. Sample Municipal"
              maxLength={100}
            />
          </label>

          <div className="space-y-2">
            <span className="text-sm font-medium text-stone-700">Holes</span>
            <div className="grid grid-cols-2 gap-3">
              {[9, 18].map((count) => {
                const selected = holeCount === count
                return (
                  <button
                    key={count}
                    type="button"
                    onClick={() => updateHoleCount(count as 9 | 18)}
                    className={`rounded-xl border px-4 py-3 text-base font-medium transition ${
                      selected
                        ? 'border-emerald-700 bg-emerald-50 text-emerald-900'
                        : 'border-stone-300 bg-white text-stone-700 hover:bg-stone-50'
                    }`}
                  >
                    {count} holes
                  </button>
                )
              })}
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-stone-900">Hole pars</h2>
          <span className="text-sm text-stone-500">Quick defaults to par 4</span>
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {holes.map((hole) => (
            <label
              key={hole.holeNumber}
              className="flex items-center justify-between rounded-xl border border-stone-200 px-4 py-3"
            >
              <span className="text-sm font-medium text-stone-700">Hole {hole.holeNumber}</span>
              <select
                value={hole.par}
                onChange={(event) => updatePar(hole.holeNumber, Number(event.target.value))}
                className="rounded-lg border border-stone-300 px-3 py-2 text-sm focus:border-emerald-600"
              >
                {[3, 4, 5, 6].map((par) => (
                  <option key={par} value={par}>
                    Par {par}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
      </div>

      {error ? <p className="text-sm font-medium text-red-600">{error}</p> : null}

      <div className="sticky bottom-4 flex gap-3 rounded-2xl border border-stone-200 bg-white/95 p-3 shadow-lg backdrop-blur">
        <button
          type="submit"
          disabled={isSaving}
          className="min-h-[56px] flex-1 rounded-xl bg-emerald-700 px-4 py-3 text-base font-semibold text-white transition hover:bg-emerald-800 active:scale-[0.99] disabled:cursor-not-allowed disabled:opacity-60"
        >
          {isSaving ? 'Saving…' : mode === 'create' ? 'Save Course' : 'Save Changes'}
        </button>
        <button
          type="button"
          onClick={() => router.back()}
          className="min-h-[56px] rounded-xl border border-stone-300 px-4 py-3 text-base font-medium text-stone-700 transition hover:bg-stone-50 active:scale-[0.99]"
        >
          Cancel
        </button>
      </div>
    </form>
  )
}
