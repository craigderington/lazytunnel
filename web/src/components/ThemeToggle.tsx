import { Moon, Sun } from 'lucide-react'
import { useThemeStore } from '@/store/themeStore'
import { Button } from './ui/button'

export function ThemeToggle() {
  const { theme, toggleTheme } = useThemeStore()

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggleTheme}
      className="relative overflow-hidden"
      title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`}
    >
      <Sun
        className={`h-5 w-5 transition-all ${
          theme === 'light'
            ? 'rotate-0 scale-100'
            : 'rotate-90 scale-0 absolute'
        }`}
      />
      <Moon
        className={`h-5 w-5 transition-all ${
          theme === 'dark'
            ? 'rotate-0 scale-100'
            : '-rotate-90 scale-0 absolute'
        }`}
      />
      <span className="sr-only">Toggle theme</span>
    </Button>
  )
}
