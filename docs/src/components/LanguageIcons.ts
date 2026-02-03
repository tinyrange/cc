// Official language logos from branding guidelines
// Go: https://go.dev/blog/go-brand
// Python: https://www.python.org/community/logos/
// Rust: https://github.com/rust-lang/rust-artwork (CC-BY)
// TypeScript: https://www.typescriptlang.org/branding/
// C: Wikimedia Commons (no official logo)

export const icons = {
  // Official Go wordmark from go.dev (simplified paths, uses brand color #00ADD8)
  go: `<svg viewBox="0 0 254.5 225" xmlns="http://www.w3.org/2000/svg">
    <g fill="#00ADD8">
      <path d="M40.2,101.1c-0.4,0-0.5-0.2-0.3-0.5l2.1-2.7c0.2-0.3,0.7-0.5,1.1-0.5l35.7,0c0.4,0,0.5,0.3,0.3,0.6l-1.7,2.6c-0.2,0.3-0.7,0.6-1,0.6L40.2,101.1z"/>
      <path d="M25.1,110.3c-0.4,0-0.5-0.2-0.3-0.5l2.1-2.7c0.2-0.3,0.7-0.5,1.1-0.5l45.6,0c0.4,0,0.6,0.3,0.5,0.6l-0.8,2.4c-0.1,0.4-0.5,0.6-0.9,0.6L25.1,110.3z"/>
      <path d="M49.3,119.5c-0.4,0-0.5-0.3-0.3-0.6l1.4-2.5c0.2-0.3,0.6-0.6,1-0.6l20,0c0.4,0,0.6,0.3,0.6,0.7l-0.2,2.4c0,0.4-0.4,0.7-0.7,0.7L49.3,119.5z"/>
      <path d="M153.1,99.3c-6.3,1.6-10.6,2.8-16.8,4.4c-1.5,0.4-1.6,0.5-2.9-1c-1.5-1.7-2.6-2.8-4.7-3.8c-6.3-3.1-12.4-2.2-18.1,1.5c-6.8,4.4-10.3,10.9-10.2,19c0.1,8,5.6,14.6,13.5,15.7c6.8,0.9,12.5-1.5,17-6.6c0.9-1.1,1.7-2.3,2.7-3.7c-3.6,0-8.1,0-19.3,0c-2.1,0-2.6-1.3-1.9-3c1.3-3.1,3.7-8.3,5.1-10.9c0.3-0.6,1-1.6,2.5-1.6c5.1,0,23.9,0,36.4,0c-0.2,2.7-0.2,5.4-0.6,8.1c-1.1,7.2-3.8,13.8-8.2,19.6c-7.2,9.5-16.6,15.4-28.5,17c-9.8,1.3-18.9-0.6-26.9-6.6c-7.4-5.6-11.6-13-12.7-22.2c-1.3-10.9,1.9-20.7,8.5-29.3c7.1-9.3,16.5-15.2,28-17.3c9.4-1.7,18.4-0.6,26.5,4.9c5.3,3.5,9.1,8.3,11.6,14.1C154.7,98.5,154.3,99,153.1,99.3z"/>
      <path d="M186.2,154.6c-9.1-0.2-17.4-2.8-24.4-8.8c-5.9-5.1-9.6-11.6-10.8-19.3c-1.8-11.3,1.3-21.3,8.1-30.2c7.3-9.6,16.1-14.6,28-16.7c10.2-1.8,19.8-0.8,28.5,5.1c7.9,5.4,12.8,12.7,14.1,22.3c1.7,13.5-2.2,24.5-11.5,33.9c-6.6,6.7-14.7,10.9-24,12.8C191.5,154.2,188.8,154.3,186.2,154.6z M210,114.2c-0.1-1.3-0.1-2.3-0.3-3.3c-1.8-9.9-10.9-15.5-20.4-13.3c-9.3,2.1-15.3,8-17.5,17.4c-1.8,7.8,2,15.7,9.2,18.9c5.5,2.4,11,2.1,16.3-0.6C205.2,129.2,209.5,122.8,210,114.2z"/>
    </g>
  </svg>`,

  // Official Python logo from python.org (simplified two-snake design)
  python: `<svg viewBox="0 0 110 110" xmlns="http://www.w3.org/2000/svg">
    <defs>
      <linearGradient id="pyBlue" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" style="stop-color:#5A9FD4"/>
        <stop offset="100%" style="stop-color:#306998"/>
      </linearGradient>
      <linearGradient id="pyYellow" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" style="stop-color:#FFD43B"/>
        <stop offset="100%" style="stop-color:#FFE873"/>
      </linearGradient>
    </defs>
    <path fill="url(#pyBlue)" d="M54.3,12.5c-4.6,0.02-8.9,0.4-12.8,1.1C30.2,15.5,28.1,19.7,28.1,27.4v10.2h26.8v3.4H28.1H18c-7.8,0-14.6,4.7-16.7,13.6c-2.5,10.2-2.6,16.6,0,27.2c1.9,7.9,6.5,13.6,14.2,13.6h9.2V83.2c0-8.8,7.7-16.7,16.8-16.7h26.8c7.5,0,13.4-6.1,13.4-13.6V27.4c0-7.3-6.1-12.7-13.4-13.9c-4.6-0.8-9.4-1-14-1zm-14.5,8.2c2.8,0,5,2.3,5,5.1c0,2.8-2.3,5.1-5,5.1c-2.8,0-5-2.3-5-5.1C34.8,23,37,20.7,39.8,20.7z"/>
    <path fill="url(#pyYellow)" d="M85,40.5v11.9c0,9.2-7.8,17-16.8,17H41.4c-7.3,0-13.4,6.3-13.4,13.6v25.5c0,7.3,6.3,11.5,13.4,13.6c8.5,2.5,16.6,3,26.8,0c6.8-2,13.4-5.9,13.4-13.6V98H54.8v-3.4h26.8h13.4c7.8,0,10.7-5.4,13.4-13.6c2.8-8.4,2.7-16.5,0-27.2c-1.9-7.8-5.6-13.6-13.4-13.6H85V40.5z M70.3,105.3c2.8,0,5,2.3,5,5.1c0,2.8-2.3,5.1-5,5.1c-2.8,0-5-2.3-5-5.1C65.3,107.6,67.5,105.3,70.3,105.3z"/>
  </svg>`,

  // Official Rust logo from rust-lang/rust-artwork (CC-BY license)
  rust: `<svg viewBox="0 0 106 106" xmlns="http://www.w3.org/2000/svg" fill="currentColor">
    <g transform="translate(53, 53)">
      <path stroke="currentColor" stroke-width="1" stroke-linejoin="round" d="M -9,-15 H 4 C 12,-15 12,-7 4,-7 H -9 Z M -40,22 H 0 V 11 H -9 V 3 H 1 C 12,3 6,22 15,22 H 40 V 3 H 34 V 5 C 34,13 25,12 24,7 C 23,2 19,-2 18,-2 C 33,-10 24,-26 12,-26 H -35 V -15 H -25 V 11 H -40 Z"/>
      <g>
        <circle r="43" fill="none" stroke="currentColor" stroke-width="9"/>
        <g id="cogs">
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(22.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(45)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(67.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(90)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(112.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(135)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(157.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(180)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(202.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(225)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(247.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(270)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(292.5)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(315)"/>
          <polygon stroke="currentColor" stroke-width="3" stroke-linejoin="round" points="46,3 51,0 46,-3" transform="rotate(337.5)"/>
        </g>
        <g>
          <polygon stroke="currentColor" stroke-width="6" stroke-linejoin="round" points="-7,-42 0,-35 7,-42"/>
          <polygon stroke="currentColor" stroke-width="6" stroke-linejoin="round" points="-7,-42 0,-35 7,-42" transform="rotate(72)"/>
          <polygon stroke="currentColor" stroke-width="6" stroke-linejoin="round" points="-7,-42 0,-35 7,-42" transform="rotate(144)"/>
          <polygon stroke="currentColor" stroke-width="6" stroke-linejoin="round" points="-7,-42 0,-35 7,-42" transform="rotate(216)"/>
          <polygon stroke="currentColor" stroke-width="6" stroke-linejoin="round" points="-7,-42 0,-35 7,-42" transform="rotate(288)"/>
        </g>
      </g>
    </g>
  </svg>`,

  // Official TypeScript logo from typescriptlang.org/branding
  typescript: `<svg viewBox="0 0 512 512" xmlns="http://www.w3.org/2000/svg">
    <rect fill="#3178c6" height="512" rx="50" width="512"/>
    <path clip-rule="evenodd" d="m316.939 407.424v50.061c8.138 4.172 17.763 7.3 28.875 9.386s22.823 3.129 35.135 3.129c11.999 0 23.397-1.147 34.196-3.442 10.799-2.294 20.268-6.075 28.406-11.342 8.138-5.266 14.581-12.15 19.328-20.65s7.121-19.007 7.121-31.522c0-9.074-1.356-17.026-4.069-23.857s-6.625-12.906-11.738-18.225c-5.112-5.319-11.242-10.091-18.389-14.315s-15.207-8.213-24.18-11.967c-6.573-2.712-12.468-5.345-17.685-7.9-5.217-2.556-9.651-5.163-13.303-7.822-3.652-2.66-6.469-5.476-8.451-8.448-1.982-2.973-2.974-6.336-2.974-10.091 0-3.441.887-6.544 2.661-9.308s4.278-5.136 7.512-7.118c3.235-1.981 7.199-3.52 11.894-4.615 4.696-1.095 9.912-1.642 15.651-1.642 4.173 0 8.581.313 13.224.938 4.643.626 9.312 1.591 14.008 2.894 4.695 1.304 9.259 2.947 13.694 4.928 4.434 1.982 8.529 4.276 12.285 6.884v-46.776c-7.616-2.92-15.937-5.084-24.962-6.492s-19.381-2.112-31.066-2.112c-11.895 0-23.163 1.278-33.805 3.833s-20.006 6.544-28.093 11.967c-8.086 5.424-14.476 12.333-19.171 20.729-4.695 8.395-7.043 18.433-7.043 30.114 0 14.914 4.304 27.638 12.912 38.172 8.607 10.533 21.675 19.45 39.204 26.751 6.886 2.816 13.303 5.579 19.25 8.291s11.086 5.528 15.415 8.448c4.33 2.92 7.747 6.101 10.252 9.543 2.504 3.441 3.756 7.352 3.756 11.733 0 3.233-.783 6.231-2.348 8.995s-3.939 5.162-7.121 7.196-7.147 3.624-11.894 4.771c-4.748 1.148-10.303 1.721-16.668 1.721-10.851 0-21.597-1.903-32.24-5.71-10.642-3.806-20.502-9.516-29.579-17.13zm-84.159-123.342h64.22v-41.082h-179v41.082h63.906v182.918h50.874z" fill="#fff" fill-rule="evenodd"/>
  </svg>`,

  // C Programming Language logo (hexagon style from Wikimedia Commons)
  c: `<svg viewBox="0 0 38 42" xmlns="http://www.w3.org/2000/svg">
    <path fill="#004482" d="m 17.903,0.286 c 0.679,-0.381 1.515,-0.381 2.193,0 C 23.451,2.169 33.547,7.837 36.903,9.72 37.582,10.1 38,10.804 38,11.566 c 0,3.766 0,15.101 0,18.867 0,0.762 -0.418,1.466 -1.097,1.847 -3.355,1.883 -13.451,7.551 -16.807,9.434 -0.679,0.381 -1.515,0.381 -2.193,0 -3.355,-1.883 -13.451,-7.551 -16.807,-9.434 -0.678,-0.381 -1.096,-1.084 -1.096,-1.846 0,-3.766 0,-15.101 0,-18.867 0,-0.762 0.418,-1.466 1.097,-1.847 3.354,-1.883 13.452,-7.551 16.806,-9.434 z"/>
    <path fill="#659AD2" d="m 0.304,31.404 c -0.266,-0.356 -0.304,-0.694 -0.304,-1.149 0,-3.744 0,-15.014 0,-18.759 0,-0.758 0.417,-1.458 1.094,-1.836 3.343,-1.872 13.405,-7.507 16.748,-9.38 0.677,-0.379 1.594,-0.371 2.271,0.008 3.343,1.872 13.371,7.459 16.714,9.331 0.27,0.152 0.476,0.335 0.66,0.576 z"/>
    <path fill="#fff" d="m 19,7 c 7.727,0 14,6.273 14,14 0,7.727 -6.273,14 -14,14 -7.727,0 -14,-6.273 -14,-14 0,-7.727 6.273,-14 14,-14 z m 0,7 c 3.863,0 7,3.136 7,7 0,3.863 -3.137,7 -7,7 -3.863,0 -7,-3.137 -7,-7 0,-3.864 3.136,-7 7,-7 z"/>
    <path fill="#00599C" d="m 37.485,10.205 c 0.516,0.483 0.506,1.211 0.506,1.784 0,3.795 -0.032,14.589 0.009,18.384 0.004,0.396 -0.127,0.813 -0.323,1.127 l -19.084,-10.5 z"/>
  </svg>`,
} as const;

export type LanguageKey = keyof typeof icons;

export function getLanguageIcon(lang: LanguageKey): string {
  return icons[lang];
}
