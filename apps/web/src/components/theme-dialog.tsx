import { Check, Laptop, Moon, Sun } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { resolveTheme, SKINS, skinOf, useTheme, type ResolvedTheme, type Skin, type Theme } from './theme-provider'

const MODES = [
  { value: 'light', label: 'Claro', icon: Sun },
  { value: 'dark', label: 'Escuro', icon: Moon },
  { value: 'system', label: 'Sistema', icon: Laptop },
] as const satisfies ReadonlyArray<{ value: Theme; label: string; icon: typeof Sun }>

const Line = ({ className }: { className: string }) => <span className={cn('block h-1 rounded-full', className)} />

/** A tiny workspace painted with the skin's real tokens: the data attributes scope them to this box. */
const SkinPreview = ({ skin, theme, reduced }: { skin: Skin; theme: ResolvedTheme; reduced: boolean }) => (
  <div
    data-skin={skin}
    data-theme={theme}
    data-transparency={reduced ? 'reduced' : undefined}
    aria-hidden
    className="wallpaper h-24 overflow-hidden rounded-lg"
  >
    <div className="flex size-full bg-surface-content glass:gap-1 glass:bg-transparent glass:p-1">
      <div className="flex w-6 shrink-0 flex-col items-center gap-1 bg-surface-rail pt-1.5 glass:glass-panel glass:rounded-lg">
        <span className="size-3.5 rounded-full bg-blurple" />
        <span className="size-3.5 rounded-full bg-surface-raised" />
      </div>
      <div className="w-16 shrink-0 space-y-1.5 bg-surface-sidebar p-2 glass:glass-panel glass:rounded-lg">
        <Line className="w-3/4 bg-greyple/70" />
        <Line className="bg-surface-active" />
        <Line className="w-2/3 bg-greyple/70" />
      </div>
      <div className="flex min-w-0 flex-1 flex-col justify-end gap-1.5 p-2 glass:glass-panel glass:glass-strong glass:rounded-lg">
        <span className="flex items-center gap-1.5">
          <span className="size-3 shrink-0 rounded-full bg-fuchsia" />
          <Line className="w-1/2 bg-snow" />
        </span>
        <span className="flex items-center gap-1.5">
          <span className="size-3 shrink-0 rounded-full bg-cerulean" />
          <Line className="w-2/3 bg-fog/80" />
        </span>
        <span className="mt-0.5 h-3 rounded-sm bg-surface-raised" />
      </div>
    </div>
  </div>
)

/** "Aparência": skin, color mode and the glass fallback. */
export const ThemeDialog = ({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) => {
  const { skin, setSkin, theme, setTheme, reduceTransparency, setReduceTransparency } = useTheme()
  const systemDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const singleMode = skinOf(skin).modes.length === 1

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-5 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-lg font-bold text-snow">Aparência</DialogTitle>
          <DialogDescription>Escolha o tema e o modo de cor do Discordo.</DialogDescription>
        </DialogHeader>

        <section className="space-y-2">
          <h3 className="section-label">Tema</h3>
          <div role="radiogroup" aria-label="Tema" className="grid grid-cols-2 gap-3">
            {SKINS.map((s) => (
              <button
                key={s.id}
                type="button"
                role="radio"
                aria-checked={skin === s.id}
                onClick={() => setSkin(s.id)}
                className={cn(
                  'rounded-xl p-1.5 text-left ring-1 ring-border transition-shadow hover:ring-fog/40',
                  skin === s.id && 'ring-2 ring-blurple hover:ring-blurple',
                )}
              >
                <SkinPreview skin={s.id} theme={resolveTheme(s.id, theme, systemDark)} reduced={reduceTransparency} />
                <span className="flex items-center gap-1.5 px-1 pt-2 text-sm font-semibold text-snow">
                  {s.label}
                  {skin === s.id ? <Check aria-hidden className="size-4 text-blurple-soft" /> : null}
                </span>
                <span className="block px-1 pb-1 text-xs text-greyple">{s.description}</span>
              </button>
            ))}
          </div>
        </section>

        <section className="space-y-2">
          <h3 className="section-label">Modo</h3>
          <div
            role="radiogroup"
            aria-label="Modo de cor"
            className="grid grid-cols-3 gap-1 rounded-xl bg-surface-input p-1"
          >
            {MODES.map(({ value, label, icon: Icon }) => (
              <Button
                key={value}
                type="button"
                role="radio"
                aria-checked={theme === value}
                disabled={singleMode}
                size="sm"
                variant={theme === value ? 'default' : 'ghost'}
                onClick={() => setTheme(value)}
              >
                <Icon aria-hidden />
                {label}
              </Button>
            ))}
          </div>
          {singleMode ? (
            <p className="text-xs text-greyple">O tema {skinOf(skin).label} só tem a versão escura.</p>
          ) : null}
        </section>

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="reduce-transparency" className="flex-col items-start gap-0.5">
            <span className="text-sm font-semibold text-snow">Reduzir transparência</span>
            <span className="text-xs font-normal text-greyple">
              Painéis sólidos, sem desfoque. Vale para o Liquid Glass.
            </span>
          </Label>
          <Switch id="reduce-transparency" checked={reduceTransparency} onCheckedChange={setReduceTransparency} />
        </div>
      </DialogContent>
    </Dialog>
  )
}
