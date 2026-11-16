import { createContext, useContext, useLayoutEffect, useMemo, useState, type ReactNode } from 'react'

export type Theme = 'dark' | 'light' | 'system'
export type ResolvedTheme = Exclude<Theme, 'system'>

/** A skin is the look (tokens in design/tokens.css); the color mode applies only where the skin has both.
 * index.html repeats `id`, `modes` and `themeColor` to paint the right skin before React loads. */
export const SKINS = [
  { id: 'default', label: 'Discordo', description: 'O visual clássico, escuro.', modes: ['dark'], themeColor: { dark: '#1e1f22' } },
  { id: 'glass', label: 'Liquid Glass', description: 'Painéis de vidro sobre um fundo colorido.', modes: ['light', 'dark'], themeColor: { dark: '#0b0c1a', light: '#eef0f8' } },
] as const satisfies ReadonlyArray<SkinInfo>

type SkinInfo = { id: string; label: string; description: string; modes: readonly [ResolvedTheme, ...ResolvedTheme[]]; themeColor: Partial<Record<ResolvedTheme, string>> }
export type Skin = (typeof SKINS)[number]['id']

type ThemeContextValue = {
  skin: Skin
  setSkin: (skin: Skin) => void
  theme: Theme
  resolvedTheme: ResolvedTheme
  setTheme: (theme: Theme) => void
  reduceTransparency: boolean
  setReduceTransparency: (reduce: boolean) => void
}

const KEYS = { theme: 'discordo-theme', skin: 'discordo-skin', reduceTransparency: 'discordo-reduce-transparency' } as const
const DARK_QUERY = '(prefers-color-scheme: dark)'
const LESS_GLASS_QUERY = '(prefers-reduced-transparency: reduce)'
const ThemeContext = createContext<ThemeContextValue | null>(null)

export const skinOf = (id: Skin): SkinInfo => SKINS.find((s) => s.id === id) ?? SKINS[0]

/** The mode a skin actually shows: a dark-only skin ignores the preference. */
export const resolveTheme = (skin: Skin, theme: Theme, systemDark: boolean): ResolvedTheme => {
  const wanted = theme === 'system' ? (systemDark ? 'dark' : 'light') : theme
  const { modes } = skinOf(skin)
  return modes.includes(wanted) ? wanted : modes[0]
}

const read = (key: string) => {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

const write = (key: string, value: string) => {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Without storage the choice still holds for this session.
  }
}

const storedTheme = (): Theme => {
  const value = read(KEYS.theme)
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}

const storedSkin = (): Skin => SKINS.find((s) => s.id === read(KEYS.skin))?.id ?? 'default'

export const ThemeProvider = ({ children }: { children: ReactNode }) => {
  const [skin, setSkinState] = useState<Skin>(storedSkin)
  const [theme, setThemeState] = useState<Theme>(storedTheme)
  const [reduceTransparency, setReduceState] = useState(() => read(KEYS.reduceTransparency) === 'true')
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() => resolveTheme(skin, theme, window.matchMedia(DARK_QUERY).matches))

  useLayoutEffect(() => {
    const root = document.documentElement
    const dark = window.matchMedia(DARK_QUERY)
    const lessGlass = window.matchMedia(LESS_GLASS_QUERY)
    const apply = () => {
      const resolved = resolveTheme(skin, theme, dark.matches)
      root.dataset.skin = skin
      root.dataset.theme = resolved
      root.classList.toggle('dark', resolved === 'dark')
      if (reduceTransparency || lessGlass.matches) root.dataset.transparency = 'reduced'
      else delete root.dataset.transparency
      document.querySelector('meta[name="theme-color"]')?.setAttribute('content', skinOf(skin).themeColor[resolved] ?? '')
      setResolvedTheme(resolved)
    }

    apply()
    dark.addEventListener('change', apply)
    lessGlass.addEventListener('change', apply)
    return () => {
      dark.removeEventListener('change', apply)
      lessGlass.removeEventListener('change', apply)
    }
  }, [skin, theme, reduceTransparency])

  const value = useMemo<ThemeContextValue>(
    () => ({
      skin,
      setSkin: (next) => {
        write(KEYS.skin, next)
        setSkinState(next)
      },
      theme,
      resolvedTheme,
      setTheme: (next) => {
        write(KEYS.theme, next)
        setThemeState(next)
      },
      reduceTransparency,
      setReduceTransparency: (next) => {
        write(KEYS.reduceTransparency, String(next))
        setReduceState(next)
      },
    }),
    [skin, theme, resolvedTheme, reduceTransparency],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export const useTheme = () => {
  const context = useContext(ThemeContext)
  if (!context) throw new Error('useTheme precisa estar dentro de ThemeProvider')
  return context
}
