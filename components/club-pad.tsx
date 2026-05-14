'use client'

import { DEFAULT_CLUBS } from '@/lib/clubs'

type ClubPadProps = {
  onSelectClub: (club: string) => void
  disabled?: boolean
}

export function ClubPad({ onSelectClub, disabled = false }: ClubPadProps) {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {DEFAULT_CLUBS.map((club) => (
        <button
          key={club}
          type="button"
          disabled={disabled}
          onClick={() => onSelectClub(club)}
          className="min-h-[56px] rounded-2xl border border-stone-300 bg-white px-4 py-4 text-base font-semibold text-stone-900 shadow-sm transition hover:border-emerald-400 hover:bg-emerald-50 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-60"
        >
          {club}
        </button>
      ))}
    </div>
  )
}
