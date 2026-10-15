export function Logo({ size = 32, className = '' }: { size?: number; className?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      className={className}
      aria-hidden="true"
    >
      <defs>
        <linearGradient id="hf-logo-grad" x1="5" y1="3" x2="27" y2="29" gradientUnits="userSpaceOnUse">
          <stop stopColor="rgb(var(--brand-soft))" />
          <stop offset="1" stopColor="rgb(var(--brand-deep))" />
        </linearGradient>
      </defs>
      <path
        d="M16 2.6 L27.4 9.3 L27.4 22.7 L16 29.4 L4.6 22.7 L4.6 9.3 Z"
        stroke="url(#hf-logo-grad)"
        strokeWidth="2.1"
        strokeLinejoin="round"
      />
      <path
        d="M16 9 C16.75 13.1 18.9 15.25 23 16 C18.9 16.75 16.75 18.9 16 23 C15.25 18.9 13.1 16.75 9 16 C13.1 15.25 15.25 13.1 16 9 Z"
        fill="url(#hf-logo-grad)"
      />
    </svg>
  )
}
