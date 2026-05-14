import Link from 'next/link'
import { notFound } from 'next/navigation'

import { AppShell } from '@/components/app-shell'
import { getCourseById } from '@/lib/courses'

export const dynamic = 'force-dynamic'

export default async function CourseDetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = await params
  const course = await getCourseById(Number(id))

  if (!course) {
    notFound()
  }

  const totalPar = course.holes.reduce((sum, hole) => sum + hole.par, 0)

  return (
    <AppShell>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-emerald-700">Course</p>
          <h1 className="text-2xl font-semibold tracking-tight text-stone-900">{course.name}</h1>
          <p className="mt-1 text-sm text-stone-600">
            {course.holeCount} holes · Total par {totalPar}
          </p>
        </div>
        <Link
          href={`/courses/${course.id}/edit`}
          className="rounded-xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
        >
          Edit Course
        </Link>
      </div>

      <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
        <h2 className="mb-4 text-lg font-semibold text-stone-900">Hole pars</h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {course.holes.map((hole) => (
            <div
              key={hole.id}
              className="flex items-center justify-between rounded-xl border border-stone-200 px-4 py-3"
            >
              <span className="text-sm font-medium text-stone-700">Hole {hole.holeNumber}</span>
              <span className="rounded-full bg-emerald-50 px-3 py-1 text-sm font-semibold text-emerald-800">
                Par {hole.par}
              </span>
            </div>
          ))}
        </div>
      </section>
    </AppShell>
  )
}
