/**
 * Prettier (ENT-214).
 *
 * A config file rather than a `prettier` key in package.json, because the
 * choices below are all arguable and the reasons are worth more than the
 * values. Prettier's own defaults are used wherever the codebase had no
 * settled habit; the two overrides exist because it did.
 *
 * The style was measured, not chosen. Across the tracked TypeScript before
 * the sweep: 295 imports without a trailing semicolon against 35 with, and
 * 234 single-quoted module specifiers against 96 double-quoted. Formatting
 * to the minority style would have produced a larger diff and surprised more
 * of the codebase, so the majority won on both counts. The point of a
 * formatter is to stop the argument, not to win it.
 *
 * `printWidth` stays at Prettier's default 80. Only 5% of lines exceeded it,
 * the repository's prose and comments already wrap near there, and a default
 * is one less number for a contributor to look up.
 */
const config = {
  semi: false,
  singleQuote: true,
}

export default config
