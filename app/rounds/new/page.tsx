import Link from 'next/link'

import { AppShell } from '@/components/app-shell'
import { SectionCard } from '@/components/section-card'
import { StartRoundForm } from '@/components/start-round-form'
import { listCourses } from '@/lib/courses'
import { getRoundPlayLabel } from '@/lib/round-play'
import { getInProgressRound } from '@/lib/rounds'

export const dynamic = 'force-dynamic'

export default async function NewRoundPage() {
  const [courses, inProgressRound] = await Promise.all([listCourses(), getInProgressRound()])

  return (
    <AppShell>
      {inProgressRound ? (
        <SectionCard
          title="Round already in progress"
          description="Finish or continue your active round before starting a new one."
        >
          <div className="space-y-3">
            <p className="text-sm text-stone-600">
              {inProgressRound.course.name}
              {' · '}
              {getRoundPlayLabel(
                inProgressRound.course.holeCount,
                inProgressRound.holes.map((hole) => hole.holeNumber),
              )}
              {' · '}Currently on hole {inProgressRound.currentHole}
            </p>
            <div className="flex flex-wrap gap-3">
              <Link
                href={`/rounds/${inProgressRound.id}/play`}
                className="rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white transition hover:bg-emerald-800"
              >
                Continue Round
              </Link>
              <Link
                href="/rounds"
                className="rounded-xl border border-stone-300 px-4 py-3 text-sm font-semibold text-stone-700 transition hover:bg-stone-50"
              >
                View Past Rounds
              </Link>
            </div>
          </div>
        </SectionCard>
      ) : courses.length === 0 ? (
        <SectionCard
          title="No courses available"
          description="Add a course before you start your first round."
        >
          <Link
            href="/courses/new"
            className="inline-flex rounded-xl bg-emerald-700 px-4 py-3 text-sm font-semibold text-white transition hover:bg-emerald-800"
          >
            Add Course
          </Link>
        </SectionCard>
      ) : (
        <StartRoundForm courses={courses} />
      )}
    </AppShell>
  )
}
