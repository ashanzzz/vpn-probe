/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#ebf5ff',
          100: '#e1effe',
          200: '#c3ddfd',
          500: '#1a73e8',
          600: '#0969da',
          700: '#0654b0',
        },
        status: {
          online: '#1a7f37',
          offline: '#cf222e',
          blocked: '#cf222e',
          china: '#9a6700',
          clean: '#1a7f37',
          speed: '#0969da',
        }
      },
      fontSize: {
        '3xs': ['0.5625rem', { lineHeight: '0.75rem' }], // 9px
        '2xs': ['0.625rem', { lineHeight: '0.875rem' }],  // 10px
      },
      fontFamily: {
        mono: ['"JetBrains Mono"', 'Consolas', 'Monaco', 'monospace'],
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', '"Noto Sans"', 'Helvetica', 'Arial', 'sans-serif'],
      }
    },
  },
  plugins: [],
}
