/**
 * Shared Tailwind theme tokens for the workspace, management pages, and chat.
 * Semantic token names are retained across components, so changing a token updates
 * the same surfaces consistently. Source scanning covers the HTML entry and all
 * TypeScript/TSX component files.
 */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      /** Semantic color names are consumed by shared classes and feature pages. */
      colors: {
        void: '#edf3fb',
        carbon: '#ffffff',
        obsidian: '#ffffff',
        graphite: '#f0f5fc',
        smoke: '#dbe5f2',
        ash: '#66768d',
        fog: '#586c89',
        mist: '#354968',
        bone: '#152b4b',
        paper: '#ffffff',
        lime: '#2764e7',
        teal: '#367b9f',
        coral: '#cc4558',
        pulse: '#347b9c',
        violet: '#6377b0',
      },
      /** Locally hosted Latin fonts fall back to platform fonts for other scripts. */
      fontFamily: {
        sans: [
          'Space Grotesk',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif',
        ],
        mono: ['JetBrains Mono', 'Menlo', 'Consolas', 'monospace'],
      },
      boxShadow: {
        panel: '0 8px 30px -18px rgba(35, 68, 119, 0.18), inset 0 1px 0 rgba(255, 255, 255, 0.95)',
        float: '0 24px 64px -28px rgba(35, 68, 119, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.9)',
      },
      /** Shared easing matches the short entry transitions used by UI controls. */
      transitionTimingFunction: {
        'out-expo': 'cubic-bezier(0.16, 1, 0.3, 1)',
      },
      /** Blinking caret is attached only to live assistant content. */
      keyframes: {
        'blink-caret': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0' },
        },
      },
      animation: {
        'blink-caret': 'blink-caret 1s step-end infinite',
      },
    },
  },
  plugins: [],
};
