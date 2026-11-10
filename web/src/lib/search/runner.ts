// Runs searches so that an older, slower response can never replace a newer one.
export class SearchRunner<T> {
  #seq = 0
  #ctl: AbortController | null = null
  constructor(private fetcher: (q: string, signal: AbortSignal) => Promise<T>) {}

  /** Resolves with the result, or `null` when a newer run (or cancel) superseded this one. Errors of superseded runs are swallowed. */
  async run(q: string): Promise<T | null> {
    const seq = ++this.#seq
    this.#ctl?.abort()
    const ctl = (this.#ctl = new AbortController())
    try {
      const res = await this.fetcher(q, ctl.signal)
      return seq === this.#seq ? res : null
    } catch (e) {
      if (seq !== this.#seq) return null
      throw e
    }
  }

  cancel() {
    this.#seq++
    this.#ctl?.abort()
  }
}
