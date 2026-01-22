import { createContext, useContext, useState, useEffect, ReactNode } from 'react'

export type Theme = 'dark' | 'light' | 'fresh' | 'cartoon'

// Theme order for cycling
const THEME_ORDER: Theme[] = ['dark', 'light', 'fresh', 'cartoon']

interface ThemeContextType {
  theme: Theme
  setTheme: (theme: Theme) => void
  toggleTheme: () => void
  nextTheme: () => void
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined)

export function ThemeProvider({ children }: { children: ReactNode }) {
  // Initialize theme from localStorage or default to dark
  const [theme, setThemeState] = useState<Theme>(() => {
    const saved = localStorage.getItem('theme')
    const validThemes: Theme[] = ['dark', 'light', 'fresh', 'cartoon']
    const initialTheme = validThemes.includes(saved as Theme) ? (saved as Theme) : 'dark'
    
    // Apply theme class immediately on mount
    applyThemeClass(initialTheme)
    
    return initialTheme
  })

  // Helper function to apply theme class
  const applyThemeClass = (themeToApply: Theme) => {
    const root = document.documentElement
    // Remove all theme classes
    root.classList.remove('dark-theme', 'light-theme', 'fresh-theme', 'cartoon-theme')
    // Add the current theme class
    root.classList.add(`${themeToApply}-theme`)
  }

  // Apply theme to document when theme changes
  useEffect(() => {
    applyThemeClass(theme)
  }, [theme])

  // Save theme to localStorage whenever it changes
  const setTheme = (newTheme: Theme) => {
    setThemeState(newTheme)
    localStorage.setItem('theme', newTheme)
  }

  const toggleTheme = () => {
    const currentIndex = THEME_ORDER.indexOf(theme)
    const nextIndex = (currentIndex + 1) % THEME_ORDER.length
    setTheme(THEME_ORDER[nextIndex])
  }

  const nextTheme = () => {
    toggleTheme()
  }

  return (
    <ThemeContext.Provider value={{ theme, setTheme, toggleTheme, nextTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider')
  }
  return context
}
