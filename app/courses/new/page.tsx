import { redirect } from 'next/navigation'

import { auth } from '@/auth'
import { AppShell } from '@/components/app-shell'
import { CourseForm } from '@/components/course-form'

export default async function NewCoursePage() {
  const session = await auth()

  if (session?.user?.role !== 'ADMIN') {
    redirect('/courses')
  }

  return (
    <AppShell>
      <CourseForm mode="create" />
    </AppShell>
  )
}
