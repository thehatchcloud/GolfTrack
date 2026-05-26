import { notFound, redirect } from 'next/navigation'

import { auth } from '@/auth'
import { AppShell } from '@/components/app-shell'
import { CourseForm } from '@/components/course-form'
import { getCourseById } from '@/lib/courses'

export const dynamic = 'force-dynamic'

export default async function EditCoursePage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const session = await auth()
  if (session?.user?.role !== 'ADMIN') {
    redirect('/courses')
  }

  const { id } = await params
  const course = await getCourseById(Number(id))

  if (!course) {
    notFound()
  }

  return (
    <AppShell>
      <CourseForm
        mode="edit"
        courseId={course.id}
        initialValues={{
          name: course.name,
          holeCount: course.holeCount,
          holes: course.holes.map((hole) => ({
            holeNumber: hole.holeNumber,
            par: hole.par,
          })),
        }}
      />
    </AppShell>
  )
}
