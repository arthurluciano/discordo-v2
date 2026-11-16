import { createContext, useContext, useLayoutEffect, useMemo, useState, type ReactNode } from 'react'

export type Theme = 'dark' | 'light' | 'system'
type ResolvedTheme = Exclude<Theme, 'system'>

type ThemeContextValue = {
  theme: Theme
  resolvedTheme: ResolvedTheme
  setTheme: (theme: Theme) => void
}

const STORAGE_KEY = 'discordo-theme'
const ThemeContext = createContext<ThemeContextValue | null>(null)

const systemTheme = (): ResolvedTheme =>
  window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'

const storedTheme = (): Theme => {
  try {
    const value = window.localStorage.getItem(STORAGE_KEY)
    return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
  } catch {
    return 'system'
  }
}

const initialResolvedTheme = (theme: Theme): ResolvedTheme => {
  const applied = document.documentElement.dataset.theme
  if (applied === 'light' || applied === 'dark') return applied
  return theme === 'system' ? systemTheme() : theme
}

export const ThemeProvider = ({ children }: { children: ReactNode }) => {
  const [theme, setThemeState] = useState<Theme>(storedTheme)
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() => initialResolvedTheme(theme))

  useLayoutEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const applyTheme = () => {
      const resolved = theme === 'system' ? (media.matches ? 'dark' : 'light') : theme
      document.documentElement.classList.toggle('dark', resolved === 'dark')
      document.documentElement.dataset.theme = resolved
      setResolvedTheme(resolved)
    }

    applyTheme()
    if (theme !== 'system') return
    media.addEventListener('change', applyTheme)
    return () => media.removeEventListener('change', applyTheme)
  }, [theme])

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      resolvedTheme,
      setTheme: (nextTheme) => {
        try {
          window.localStorage.setItem(STORAGE_KEY, nextTheme)
        } catch {
          // O tema continua funcional durante a sessão mesmo sem storage persistente.
        }
        setThemeState(nextTheme)
      },
    }),
    [resolvedTheme, theme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export const useTheme = () => {
  const context = useContext(ThemeContext)
  if (!context) throw new Error('useTheme precisa estar dentro de ThemeProvider')
  return context
}
