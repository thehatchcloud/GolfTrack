import { z } from 'zod'

export const holeSchema = z.object({
  holeNumber: z.number().int().min(1),
  par: z.union([z.literal(3), z.literal(4), z.literal(5), z.literal(6)]),
})

export const courseInputSchema =
  z.object({
    name: z.string().trim().min(1, 'Course name is required').max(100, 'Course name must be 100 characters or fewer'),
    holeCount: z.union([z.literal(9), z.literal(18)]),
    holes: z.array(holeSchema),
  })
  .superRefine((value, ctx) => {
    if (value.holes.length !== value.holeCount) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: `Expected ${value.holeCount} holes`,
        path: ['holes'],
      })
    }

    const sorted = [...value.holes].sort((a, b) => a.holeNumber - b.holeNumber)

    sorted.forEach((hole, index) => {
      const expectedHole = index + 1
      if (hole.holeNumber !== expectedHole) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Hole numbers must be sequential starting at 1',
          path: ['holes', index, 'holeNumber'],
        })
      }
    })

    const uniqueCount = new Set(value.holes.map((hole) => hole.holeNumber)).size
    if (uniqueCount !== value.holes.length) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: 'Hole numbers must be unique',
        path: ['holes'],
      })
    }
  })

export const shotInputSchema = z.object({
  club: z.string().trim().min(1, 'Club is required').max(32, 'Club name is too long'),
})

export const updateShotInputSchema = z.object({
  club: z.string().trim().min(1, 'Club is required').max(32, 'Club name is too long'),
})

export const startRoundInputSchema = z.object({
  courseId: z.number().int().positive(),
  playMode: z.union([z.literal('full'), z.literal('front9'), z.literal('back9')]).default('full'),
})

export const setCurrentHoleInputSchema = z.object({
  currentHole: z.number().int().positive(),
})

export const roundCompletionInputSchema = z.object({
  note: z.string().trim().max(1000, 'Note is too long').optional().nullable(),
})

export type CourseInput = z.infer<typeof courseInputSchema>
export type ShotInput = z.infer<typeof shotInputSchema>
export type UpdateShotInput = z.infer<typeof updateShotInputSchema>
export type StartRoundInput = z.infer<typeof startRoundInputSchema>
export type SetCurrentHoleInput = z.infer<typeof setCurrentHoleInputSchema>
export type RoundCompletionInput = z.infer<typeof roundCompletionInputSchema>
