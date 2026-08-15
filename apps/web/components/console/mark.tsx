/**
 * The Kindlast mark, at console size.
 *
 * Extracted from chrome.tsx when the sidebar replaced the header (ENT-222), so
 * the shell and anything else in the console draw the same one.
 *
 * Still not shared with `app/(public)/layout.tsx`: the public header sits in a
 * different visual key, on an eggshell ground at a larger size, and a single
 * component serving both would grow props for the differences rather than
 * removing them.
 */
export function KindlastMark() {
  return (
    <svg
      aria-hidden="true"
      width="26"
      height="26"
      viewBox="0 0 56 56"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect width="56" height="56" rx="11" fill="currentColor" />
      <rect x="12" y="8" width="9" height="40" rx="2" fill="white" />
      <line
        x1="21"
        y1="28"
        x2="44"
        y2="9"
        stroke="white"
        strokeWidth="9"
        strokeLinecap="round"
      />
      <line
        x1="21"
        y1="28"
        x2="44"
        y2="47"
        stroke="white"
        strokeWidth="9"
        strokeLinecap="round"
      />
      <circle cx="21" cy="28" r="5.5" fill="#00C9A7" />
    </svg>
  )
}
