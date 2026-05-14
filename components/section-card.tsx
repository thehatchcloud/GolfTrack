import type { ReactNode } from 'react'

export function SectionCard({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section className="rounded-2xl border border-stone-200 bg-white p-5 shadow-sm">
      <div className="mb-4">
        <h2 className="text-lg font-semibold text-stone-900">{title}</h2>
        {description ? <p className="mt-1 text-sm text-stone-600">{description}</p> : null}
      </div>
      {children}
    </section>
  )
}
