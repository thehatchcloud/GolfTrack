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

test('courseInputSchema rejects par 6', () => {
  assert.throws(
    () =>
      courseInputSchema.parse({
        ...sampleCourseInput,
        holes: sampleCourseInput.holes.map((hole) =>
          hole.holeNumber === 2 ? { ...hole, par: 6 } : hole,
        ),
      }),
    /Invalid input/,
  )
})
