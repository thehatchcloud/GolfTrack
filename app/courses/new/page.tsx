import { AppShell } from '@/components/app-shell'
import { CourseForm } from '@/components/course-form'

export default function NewCoursePage() {
  return (
    <AppShell>
      <CourseForm mode="create" />
    </AppShell>
  )
}
