const units: [string, number][] = [
  ['year', 31536000],
  ['month', 2592000],
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60],
]

/** "3 days ago"; "Never" for no value; "Just now" under a minute. */
export function ago(iso?: string | null, now = Date.now()): string {
  if (!iso) return 'Never'
  const s = Math.round((now - new Date(iso).getTime()) / 1000)
  for (const [u, n] of units) {
    if (s >= n) {
      const k = Math.floor(s / n)
      return `${k} ${u}${k === 1 ? '' : 's'} ago`
    }
  }
  return 'Just now'
}

export const shortDate = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })

/**
 * A calendar day ("2026-06-01", or a timestamp starting with one) as "1 Jun 2026" in the browser's language.
 * Read as UTC so the day never shifts with the time zone. The one formatter for item details, badges and lists.
 */
export function formatDay(day: string | null | undefined): string {
  const d = new Date((day ?? '').slice(0, 10) + 'T12:00:00Z')
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
}

export const languages = [
  { value: 'en', label: 'English' },
  { value: 'de', label: 'Deutsch' },
  { value: 'tr', label: 'Türkçe' },
  { value: 'fr', label: 'Français' },
  { value: 'es', label: 'Español' },
]
export const currencies = ['EUR', 'USD', 'GBP', 'CHF', 'TRY', 'CAD', 'AUD', 'JPY'].map((c) => ({ value: c, label: c }))

export function browserLanguage(): string {
  const l = (typeof navigator === 'undefined' ? 'en' : navigator.language).slice(0, 2).toLowerCase()
  return languages.some((x) => x.value === l) ? l : 'en'
}
