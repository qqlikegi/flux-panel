import {heroui} from "@heroui/theme"

// ForwardZ 主题：中性灰阶 + 胶囊形控件 + 大圆角卡片
const lightGray = {
  50: "#FAFAFB", 100: "#F3F3F5", 200: "#E8E8EB", 300: "#D5D5DA", 400: "#A4A4AC",
  500: "#75757E", 600: "#55555D", 700: "#3F3F46", 800: "#27272B", 900: "#18181B",
}
const darkGray = {
  50: "#121214", 100: "#1B1B1F", 200: "#27272C", 300: "#393940", 400: "#5C5C66",
  500: "#8A8A94", 600: "#B0B0B8", 700: "#D0D0D6", 800: "#E6E6EA", 900: "#F5F5F7",
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
      radius: { small: "0.6rem", medium: "0.85rem", large: "1.4rem" },
      borderWidth: { small: "1px", medium: "1px", large: "2px" },
    },
    themes: {
      light: {
        colors: {
          background: "#FAFAFB",
          foreground: "#17181C",
          divider: "rgba(20, 22, 28, 0.10)",
          focus: "#17181C",
          content1: "#FFFFFF",
          content2: "#F4F4F6",
          content3: "#ECECEF",
          content4: "#E2E2E6",
          default: { ...lightGray, DEFAULT: "#E8E8EB", foreground: "#17181C" },
          primary: { ...lightGray, DEFAULT: "#1C1D22", foreground: "#FFFFFF" },
        },
      },
      dark: {
        colors: {
          background: "#0B0B0D",
          foreground: "#EDEDEF",
          divider: "rgba(255, 255, 255, 0.10)",
          focus: "#EDEDEF",
          content1: "#151518",
          content2: "#1C1C20",
          content3: "#25252A",
          content4: "#303036",
          default: { ...darkGray, DEFAULT: "#27272C", foreground: "#EDEDEF" },
          primary: { ...darkGray, DEFAULT: "#F2F2F4", foreground: "#121215" },
        },
      },
    },
  })],
}
