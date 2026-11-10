import { ApiError } from './api'

/** Splits a failure into per-field messages and one general message. */
export function explain(e: unknown): { fields: Record<string, string>; message: string } {
  if (e instanceof ApiError) {
    const fields = e.fields ?? {}
    return { fields, message: Object.keys(fields).length ? '' : e.message }
  }
  return { fields: {}, message: 'Something went wrong. Try again.' }
}

export const MIN_PASSWORD = 10

export function passwordProblem(pw: string): string {
  return pw.length < MIN_PASSWORD ? `Use at least ${MIN_PASSWORD} characters.` : ''
}
