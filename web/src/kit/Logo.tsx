// SPDX-License-Identifier: AGPL-3.0-only

// openrail's mark, Turnout: the r as a railway switch, a straight stem with a branch peeling off.
// The colours are fixed so the mark reads the same in both themes. The hairline in currentColor
// keeps the ink tile visible on a dark page and vanishes on a light one.
export function Logo({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 100 100" role="img" aria-label="openrail">
      <rect width="100" height="100" rx="22" fill="#13111c" />
      <rect x="1.5" y="1.5" width="97" height="97" rx="20.5" fill="none" stroke="currentColor" strokeOpacity="0.16" strokeWidth="3" />
      <path d="M21.62 19H40.91V81H21.62Z" fill="#ffffff" />
      <path d="M43.42 54.46V28.13Q47.79 25.04 52.8 22.92Q62.06 19 72.18 19L78.38 19V36.75L72.18 36.75Q58.91 36.75 49.52 46.14Q45.69 49.97 43.42 54.46Z" fill="#ea66ad" />
    </svg>
  );
}
