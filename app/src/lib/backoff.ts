// Capped exponential backoff with jitter. Jitter matters on mobile: when a
// gateway restarts, thousands of phones reconnect, and without it they all
// retry on the same tick.
export class Backoff {
  private attempt = 0;

  constructor(
    private readonly minMs = 500,
    private readonly maxMs = 30_000,
    private readonly random: () => number = Math.random,
  ) {}

  next(): number {
    const ceiling = Math.min(this.maxMs, this.minMs * 2 ** Math.min(this.attempt, 20));
    this.attempt++;
    return Math.round(this.minMs / 2 + this.random() * ceiling);
  }

  reset(): void {
    this.attempt = 0;
  }

  get attempts(): number {
    return this.attempt;
  }
}
