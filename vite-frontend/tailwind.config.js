import {heroui} from "@heroui/theme"

// ForwardZ 主题：iOS 风格（系统蓝强调色 + 系统灰阶 + 连续圆角）
const lightGray = {
  50: "#F9F9FB", 100: "#F2F2F7", 200: "#E5E5EA", 300: "#D1D1D6", 400: "#AEAEB2",
  500: "#8E8E93", 600: "#636366", 700: "#48484A", 800: "#3A3A3C", 900: "#1C1C1E",
}
const darkGray = {
  50: "#0D0D0F", 100: "#1C1C1E", 200: "#2C2C2E", 300: "#3A3A3C", 400: "#48484A",
  500: "#8E8E93", 600: "#AEAEB2", 700: "#C7C7CC", 800: "#E5E5EA", 900: "#F2F2F7",
}
const blue = {
  50: "#EAF3FF", 100: "#D5E8FF", 200: "#AACFFF", 300: "#7FB6FF", 400: "#3F97FF",
  500: "#007AFF", 600: "#0066D6", 700: "#0052AD", 800: "#003D82", 900: "#002957",
}
const blueDark = {
  50: "#001A38", 100: "#002957", 200: "#003D82", 300: "#0052AD", 400: "#0066D6",
  500: "#0A84FF", 600: "#409CFF", 700: "#70B5FF", 800: "#A6D1FF", 900: "#D6EAFF",
}

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    './src/layouts/**/*.{js,ts,jsx,tsx,mdx}',
    './src/pages/**/*.{js,ts,jsx,tsx,mdx}',
    './src/components/**/*.{js,ts,jsx,tsx,mdx}',
    "./node_modules/@heroui/theme/dist/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {},
  },
  darkMode: "class",
  plugins: [heroui({
    layout: {
      radius: { small: "0.625rem", medium: "0.875rem", large: "1.625rem" },
      borderWidth: { small: "1px", medium: "1px", large: "2px" },
    },
    themes: {
      light: {
        colors: {
          background: "#F2F2F7",
          foreground: "#1C1C1E",
          divider: "rgba(60, 60, 67, 0.14)",
          focus: "#007AFF",
          content1: "#FFFFFF",
          content2: "#F2F2F7",
          content3: "#E5E5EA",
          content4: "#D1D1D6",
          default: { ...lightGray, DEFAULT: "#E5E5EA", foreground: "#1C1C1E" },
          primary: { ...blue, DEFAULT: "#007AFF", foreground: "#FFFFFF" },
        },
      },
      dark: {
        colors: {
          background: "#000000",
          foreground: "#F2F2F7",
          divider: "rgba(235, 235, 245, 0.14)",
          focus: "#0A84FF",
          content1: "#1C1C1E",
          content2: "#2C2C2E",
          content3: "#3A3A3C",
          content4: "#48484A",
          default: { ...darkGray, DEFAULT: "#2C2C2E", foreground: "#F2F2F7" },
          primary: { ...blueDark, DEFAULT: "#0A84FF", foreground: "#FFFFFF" },
        },
      },
    },
  })],
}
