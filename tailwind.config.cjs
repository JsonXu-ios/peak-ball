module.exports = {
  darkMode: 'class',
  content: [
    './index.html',
    './src/App.vue',
    './src/components/**/*.{vue,ts,js}',
    './src/layouts/**/*.{vue,ts,js}',
    './src/router/**/*.{ts,js}',
    './src/store/**/*.{ts,js}',
    './src/api/**/*.{ts,js}',
    './src/views/**/*.{vue,ts,js}',
  ],
  theme: {
    extend: {
      colors: {
        primary: '#135bec',
        'background-light': '#f6f6f8',
        'background-dark': '#101622',
      },
      fontFamily: {
        display: ['Lexend', 'sans-serif'],
      },
      borderRadius: {
        DEFAULT: '0.25rem',
        lg: '0.5rem',
        xl: '0.75rem',
        full: '9999px',
      },
    },
  },
  plugins: [],
}
