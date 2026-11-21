import { useState } from 'react'
import { motion } from 'motion/react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { BrandLogo } from '@/design/brand-logo'
import { EDITORIAL_EASE, FADE_UP, useTransition } from '@/design/motion'
import { DiscordMark } from './discord-mark'

const LOGIN_ERRORS: Record<string, string> = {
  expired: 'O login demorou demais. Tente de novo.',
  cancelled: 'O login foi cancelado.',
  failed: 'O Discord não confirmou sua conta. Tente de novo.',
}

/** Reads `?login_error=` once and clears it, so a reload does not show it again. */
const takeLoginError = () => {
  const url = new URL(window.location.href)
  const reason = url.searchParams.get('login_error')
  if (reason === null) return null
  url.searchParams.delete('login_error')
  window.history.replaceState(null, '', url)

  return LOGIN_ERRORS[reason] ?? LOGIN_ERRORS.failed
}

export const LoginScreen = () => {
  const [error] = useState(takeLoginError)
  const enter = useTransition(EDITORIAL_EASE)

  return (
    <main className="relative h-full min-h-0 overflow-y-auto bg-surface-cosmic text-snow">
      <div className="pointer-events-none absolute inset-0 overflow-hidden glass:hidden" aria-hidden>
        <div className="absolute -left-40 -top-56 size-[38rem] rounded-full bg-blurple/25 blur-3xl" />
        <div className="absolute -bottom-72 right-0 size-[42rem] rounded-full bg-fuchsia/10 blur-3xl" />
        <div className="absolute inset-0 opacity-20 [background-image:radial-gradient(circle_at_center,white_1px,transparent_1px)] [background-size:28px_28px]" />
      </div>

      <div className="relative mx-auto grid min-h-full max-w-7xl items-center gap-6 px-4 py-4 sm:py-10 lg:grid-cols-[minmax(0,1.15fr)_minmax(23rem,0.7fr)] lg:gap-10 lg:px-12">
        <motion.section
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={enter}
          className="hidden lg:block"
        >
          <div className="flex items-center gap-3">
            <BrandLogo className="size-14" interactive />
            <div>
              <p className="text-xl font-bold">Discordo</p>
              <p className="text-xs font-semibold uppercase tracking-[0.12em] text-fog">Seu lugar para conversar</p>
            </div>
          </div>
          <h1 className="display-crushed mt-12 max-w-3xl text-[clamp(3.5rem,6vw,6.5rem)]">
            Onde seu grupo fica junto.
          </h1>
          <p className="mt-7 max-w-xl text-lg leading-8 text-fog">
            Entre para conversar por texto, voz ou vídeo — com a espontaneidade de estar na mesma sala.
          </p>
        </motion.section>

        <motion.div
          variants={FADE_UP}
          initial="hidden"
          animate="visible"
          transition={enter}
          className="mx-auto w-full max-w-md rounded-2xl border border-white/10 bg-surface-sidebar p-6 shadow-2xl shadow-black/30 sm:p-8 glass:glass-panel glass:glass-strong glass:rounded-[2rem]"
        >
          <div className="mb-6 text-center sm:mb-8">
            <BrandLogo className="mx-auto mb-4 size-14 sm:mb-5 sm:size-16 lg:hidden" />
            <h2 className="text-xl font-bold sm:text-2xl">Boas-vindas!</h2>
            <p className="mt-2 text-sm text-fog">Entre com sua conta do Discord para continuar.</p>
          </div>
          {error ? (
            <Alert variant="destructive" className="mb-5">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          {/* A real navigation, not fetch: Discord needs the whole window to authorize. */}
          <Button asChild size="lg" className="w-full">
            <a href="/auth/discord/login">
              <DiscordMark className="size-5" />
              Entrar com Discord
            </a>
          </Button>
          <p className="mt-5 text-center text-xs text-greyple">Usamos só seu nome e avatar do Discord.</p>
        </motion.div>
      </div>
    </main>
  )
}
