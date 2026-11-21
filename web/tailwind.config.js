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
      fontFamily: {
        mono: ['"JetBrains Mono"', 'Consolas', 'Monaco', 'monospace'],
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', '"Noto Sans"', 'Helvetica', 'Arial', 'sans-serif'],
      }
    },
  },
  plugins: [],
}
