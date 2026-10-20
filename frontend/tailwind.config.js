/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: [
          'Inter',
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Oxygen',
          'Ubuntu',
          'Cantarell',
          'Fira Sans',
          'Droid Sans',
          'Helvetica Neue',
          'sans-serif',
        ],
      },
      // Happy Hues palette 3.
      // shell: app background (super-light neutral) · canvas: surfaces/cards ·
      // wash: soft tint (chips, hovers, empty boxes) · ink: headlines & dark
      // surfaces · muted: body text · steel: borders & faint accents ·
      // brand: primary actions · coral: danger / live recording.
      colors: {
        shell: '#f5f7fa',
        canvas: '#fffffe',
        wash: '#d8eefe',
        ink: '#094067',
        muted: '#5f6c7b',
        steel: {
          DEFAULT: '#90b4ce',
          light: '#c3d5e2',
        },
        brand: {
          100: '#eaf5fe',
          300: '#8ecdfb',
          400: '#5cb3fa',
          500: '#3da9fc',
          600: '#1b7cc4',
          700: '#14639e',
        },
        coral: {
          100: '#fde5ea',
          300: '#f5a3b1',
          400: '#f47085',
          500: '#ef4565',
          600: '#d93a58',
          700: '#b92e48',
        },
      },
    },
  },
  plugins: [],
}
