import test from 'node:test'
import assert from 'node:assert/strict'

import { courseInputSchema } from '../lib/validation'
import { sampleCourseInput } from './helpers/sample-data'

test('courseInputSchema accepts a valid 9-hole course', () => {
  const parsed = courseInputSchema.parse(sampleCourseInput)

  assert.equal(parsed.name, sampleCourseInput.name)
  assert.equal(parsed.holeCount, 9)
  assert.equal(parsed.holes.length, 9)
})

test('courseInputSchema rejects an empty course name', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        name: '   ',
      }),
    /Course name is required/,
  )
})

test('courseInputSchema rejects a course name exceeding 100 characters', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        name: 'a'.repeat(101),
      }),
    /Course name must be 100 characters or fewer/,
  )
})

test('courseInputSchema accepts a course name of exactly 100 characters', () => {
  const parsed = courseInputSchema.parse({
    ...sampleCourseInput,
    name: 'a'.repeat(100),
  })
  assert.equal(parsed.name.length, 100)
})

test('courseInputSchema rejects when hole count does not match holes array', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        holes: sampleCourseInput.holes.slice(0, 8),
      }),
    /Expected 9 holes/,
  )
})

test('courseInputSchema rejects duplicate hole numbers', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        holes: [
          { holeNumber: 1, par: 4 },
          { holeNumber: 1, par: 3 },
          ...sampleCourseInput.holes.slice(2),
        ],
      }),
    /Hole numbers must be unique|Hole numbers must be sequential starting at 1/,
  )
})

test('courseInputSchema rejects non-sequential hole numbers', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        holes: sampleCourseInput.holes.map((hole) =>
          hole.holeNumber === 3 ? { ...hole, holeNumber: 4 } : hole,
        ),
      }),
    /Hole numbers must be sequential starting at 1/,
  )
})

test('courseInputSchema rejects invalid low par values', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        holes: sampleCourseInput.holes.map((hole) =>
          hole.holeNumber === 2 ? { ...hole, par: 2 } : hole,
        ),
      }),
    /Invalid input/,
  )
})

test('courseInputSchema accepts par 6', () => {
  const parsed = courseInputSchema.parse({
    ...sampleCourseInput,
    holes: sampleCourseInput.holes.map((hole) =>
      hole.holeNumber === 2 ? { ...hole, par: 6 } : hole,
    ),
  })
  assert.equal(parsed.holes.find((h) => h.holeNumber === 2)?.par, 6)
})

test('courseInputSchema rejects par 7', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        holes: sampleCourseInput.holes.map((hole) =>
          hole.holeNumber === 2 ? { ...hole, par: 7 } : hole,
        ),
      }),
    /Invalid input/,
  )
})
