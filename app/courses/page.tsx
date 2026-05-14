import Link from 'next/link'

import { AppShell } from '@/components/app-shell'
import { SectionCard } from '@/components/section-card'
import { listCourses } from '@/lib/courses'

export const dynamic = 'force-dynamic'

export default async function CoursesPage() {
  const courses = await listCourses()

  return (
    <AppShell>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-stone-900">Courses</h1>
          <p className="mt-1 text-sm text-stone-600">Manage the courses you can play rounds on.</p>
        </div>
        <Link
          href="/courses/new"
          className="rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white shadow-sm transition hover:bg-emerald-800"
        >
          Add Course
        </Link>
      </div>

      {courses.length === 0 ? (
        <SectionCard
          title="No courses yet"
          description="Add your first course to start tracking rounds."
        >
          <Link
            href="/courses/new"
            className="inline-flex rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white transition hover:bg-emerald-800"
          >
            Create a course
          </Link>
        </SectionCard>
      ) : (
        <div className="grid gap-4">
          {courses.map((course) => (
            <Link
              key={course.id}
              href={`/courses/${course.id}`}
              className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm transition hover:border-emerald-300 hover:shadow"
            >
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h2 className="text-lg font-semibold text-stone-900">{course.name}</h2>
                  <p className="mt-1 text-sm text-stone-600">
                    {course.holeCount} holes · Total par {course.totalPar}
                  </p>
                </div>
                <span className="rounded-full bg-stone-100 px-3 py-1 text-xs font-medium text-stone-700">
                  View
                </span>
              </div>
            </Link>
          ))}
        </div>
      )}
    </AppShell>
  )
}
