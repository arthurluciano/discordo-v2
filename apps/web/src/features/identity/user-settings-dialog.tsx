// User settings: profile (with a live preview of the card), account, appearance, voice and video.
import { useCallback, useState, type ComponentType, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { Check, Copy, ImageUp, LogOut, Palette, Trash2, UserRound, Volume2, IdCard } from 'lucide-react'
import { AppearanceSettings } from '@/components/appearance-settings'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { api, type ProfileInput } from '@/lib/api'
import { useCopy } from '@/lib/use-copy'
import { cn } from '@/lib/utils'
import type { User, UserProfile } from '@/lib/wire'
import { useSignOut } from '../auth/use-sign-out'
import { VoiceVideoSettings } from '../voice/device-settings-dialog'
import { bannerColor, MAX_BIO, MAX_PRONOUNS, memberSince, ProfileCardView } from './profile-card'
import { useMe } from './users'

export type SettingsSection = 'profile' | 'account' | 'appearance' | 'voice'

const SECTIONS: { value: SettingsSection; label: string; icon: ComponentType<{ className?: string }> }[] = [
  { value: 'profile', label: 'Perfil', icon: UserRound },
  { value: 'account', label: 'Minha conta', icon: IdCard },
  { value: 'appearance', label: 'Aparência', icon: Palette },
  { value: 'voice', label: 'Voz e vídeo', icon: Volume2 },
]

const BANNER_SWATCHES = [0x5865f2, 0xeb459e, 0xf23f42, 0xf0b232, 0x23a55a, 0x00a8fc, 0x9b59b6, 0x2b2d31]

/** Our own profile details, shared with the profile viewer's cache. */
export const useProfile = (userId: string | null) =>
  useQuery({
    queryKey: ['profile', userId],
    queryFn: () => api.profile(userId!),
    enabled: userId !== null,
  })

type ProfileValues = Required<ProfileInput>

const ProfileForm = ({ me, profile }: { me: User; profile: UserProfile }) => {
  const queryClient = useQueryClient()

  const defaultValues: ProfileValues = {
    display_name: me.display_name,
    avatar_url: me.avatar_url,
    pronouns: profile.pronouns,
    bio: profile.bio,
    banner_color: profile.banner_color,
  }

  const { register, handleSubmit, watch, setValue, reset, formState } = useForm<ProfileValues>({ defaultValues })
  const values = watch()

  const upload = useMutation({
    mutationFn: api.uploadImage,
    onSuccess: (url) => setValue('avatar_url', url, { shouldDirty: true }),
  })

  const save = useMutation({
    mutationFn: (input: ProfileValues) => api.updateProfile(input),
    onSuccess: async (_, input) => {
      reset(input)
      await queryClient.invalidateQueries({ queryKey: ['profile', me.id] })
    },
  })

  const pickBanner = (color: number) => setValue('banner_color', color, { shouldDirty: true })

  return (
    <form
      onSubmit={handleSubmit((input) => save.mutate(input))}
      className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]"
    >
      <FieldGroup>
        <Field>
          <FieldLabel className="section-label">Avatar</FieldLabel>

          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="secondary" size="sm" asChild disabled={upload.isPending}>
              <label className="cursor-pointer">
                {upload.isPending ? <Spinner aria-hidden /> : <ImageUp aria-hidden />}
                Trocar avatar
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/gif,image/webp"
                  className="sr-only"
                  onChange={(event) => {
                    const file = event.target.files?.[0]
                    if (file) upload.mutate(file)
                    event.target.value = ''
                  }}
                />
              </label>
            </Button>

            {values.avatar_url ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setValue('avatar_url', '', { shouldDirty: true })}
              >
                <Trash2 aria-hidden />
                Remover
              </Button>
            ) : null}
          </div>
        </Field>

        <Field>
          <FieldLabel htmlFor="profile-name" className="section-label">
            Nome de exibição
          </FieldLabel>

          <Input id="profile-name" maxLength={32} {...register('display_name', { required: true })} />
        </Field>

        <Field>
          <FieldLabel htmlFor="profile-pronouns" className="section-label">
            Pronomes
          </FieldLabel>

          <Input
            id="profile-pronouns"
            maxLength={MAX_PRONOUNS}
            placeholder="ele/dele, ela/dela, elu/delu…"
            {...register('pronouns')}
          />
        </Field>

        <Field>
          <FieldLabel htmlFor="profile-bio" className="section-label">
            Sobre mim
          </FieldLabel>

          <Textarea
            id="profile-bio"
            rows={4}
            maxLength={MAX_BIO}
            placeholder="Conte um pouco sobre você."
            className="resize-none"
            {...register('bio')}
          />

          <FieldDescription className="text-right">
            {values.bio.length}/{MAX_BIO}
          </FieldDescription>
        </Field>

        <Field>
          <FieldLabel className="section-label">Cor do banner</FieldLabel>

          <div className="flex flex-wrap items-center gap-2" role="radiogroup" aria-label="Cor do banner">
            <button
              type="button"
              role="radio"
              aria-checked={values.banner_color === 0}
              onClick={() => pickBanner(0)}
              className={cn(
                'h-8 rounded-md bg-gradient-to-br from-blurple/60 to-fuchsia/40 px-3 text-xs font-semibold text-white ring-offset-2 ring-offset-popover',
                values.banner_color === 0 && 'ring-2 ring-snow',
              )}
            >
              Padrão
            </button>

            {BANNER_SWATCHES.map((color) => (
              <button
                key={color}
                type="button"
                role="radio"
                aria-checked={values.banner_color === color}
                aria-label={bannerColor(color)!}
                onClick={() => pickBanner(color)}
                className={cn(
                  'size-8 rounded-md ring-offset-2 ring-offset-popover',
                  values.banner_color === color && 'ring-2 ring-snow',
                )}
                style={{ background: bannerColor(color)! }}
              />
            ))}

            <label className="relative grid size-8 cursor-pointer place-items-center overflow-hidden rounded-md border border-border text-xs text-fog">
              <span aria-hidden>+</span>
              <input
                type="color"
                aria-label="Outra cor"
                className="absolute inset-0 cursor-pointer opacity-0"
                value={bannerColor(values.banner_color) ?? '#5865f2'}
                // Pure black would read as 0, the default banner.
                onChange={(event) => pickBanner(parseInt(event.target.value.slice(1), 16) || 1)}
              />
            </label>
          </div>
        </Field>

        <FieldError error={upload.error ?? save.error} />

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={!formState.isDirty || upload.isPending} loading={save.isPending}>
            {save.isSuccess && !formState.isDirty ? <Check aria-hidden /> : null}
            {save.isSuccess && !formState.isDirty ? 'Salvo' : 'Salvar alterações'}
          </Button>

          {formState.isDirty ? (
            <Button type="button" variant="ghost" onClick={() => reset()}>
              Desfazer
            </Button>
          ) : null}
        </div>
      </FieldGroup>

      <div className="space-y-2">
        <h3 className="section-label">Prévia</h3>

        <ProfileCardView
          online
          profile={{
            ...profile,
            ...values,
            display_name: values.display_name || me.username,
            mutual_guild_ids: [],
          }}
        />
      </div>
    </form>
  )
}

const ProfileSection = ({ me }: { me: User }) => {
  const profile = useProfile(me.id)

  if (profile.isPending) return <Spinner aria-label="Carregando o perfil" />

  if (profile.isError) return <FieldError error={profile.error} />

  return <ProfileForm me={me} profile={profile.data} />
}

const Row = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="flex items-center justify-between gap-4 border-b border-border py-3 last:border-0">
    <div className="min-w-0">
      <p className="section-label">{label}</p>
      <div className="truncate text-sm text-snow">{children}</div>
    </div>
  </div>
)

const AccountSection = ({ me }: { me: User }) => {
  const profile = useProfile(me.id)
  const { copied, copy } = useCopy()
  const signOut = useSignOut()

  return (
    <div className="space-y-6">
      <div className="rounded-xl border border-border bg-surface-raised px-4">
        <Row label="Usuário">@{me.username}</Row>

        <Row label="Membro desde">{profile.data ? memberSince(profile.data.created_at) : '…'}</Row>

        <div className="flex items-center justify-between gap-4 py-3">
          <div className="min-w-0">
            <p className="section-label">ID do usuário</p>
            <p className="truncate font-data text-sm text-snow">{me.id}</p>
          </div>

          <Button type="button" variant="secondary" size="sm" onClick={() => void copy(me.id)}>
            {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
            {copied ? 'Copiado' : 'Copiar'}
          </Button>
        </div>
      </div>

      <p className="text-xs text-greyple">
        O login é pela sua conta do Discord. O nome de usuário vem de lá; nome de exibição e avatar trocados aqui
        continuam valendo nos próximos logins.
      </p>

      <Button type="button" variant="destructive" onClick={() => signOut.mutate()} loading={signOut.isPending}>
        <LogOut aria-hidden />
        Sair do Discordo
      </Button>
    </div>
  )
}

type UserSettingsDialogProps = {
  open: boolean
  section: SettingsSection
  onSectionChange: (section: SettingsSection) => void
  onOpenChange: (open: boolean) => void
}

export const UserSettingsDialog = ({ open, section, onSectionChange, onOpenChange }: UserSettingsDialogProps) => {
  const me = useMe()
  if (!me) return null

  const current = SECTIONS.find((s) => s.value === section)!

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[min(88vh,52rem)] gap-0 overflow-hidden bg-surface-content p-0 sm:max-w-5xl">
        <div className="flex h-full min-h-0 max-md:flex-col">
          <nav
            aria-label="Seções das configurações"
            className="flex shrink-0 gap-1 bg-surface-sidebar p-3 max-md:overflow-x-auto md:w-56 md:flex-col md:p-4"
          >
            <p className="section-label px-2 pb-1 max-md:hidden">Configurações</p>

            {SECTIONS.map(({ value, label, icon: Icon }) => (
              <button
                key={value}
                type="button"
                aria-current={section === value ? 'page' : undefined}
                onClick={() => onSectionChange(value)}
                className={cn(
                  'flex shrink-0 items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm font-medium text-fog transition-colors hover:bg-surface-hover hover:text-snow',
                  section === value && 'bg-surface-active text-snow',
                )}
              >
                <Icon className="size-4" />
                {label}
              </button>
            ))}
          </nav>

          <div className="min-h-0 flex-1 overflow-y-auto p-5 sm:p-8">
            <DialogTitle className="mb-1 text-xl font-extrabold">{current.label}</DialogTitle>

            <DialogDescription className="mb-6">
              {section === 'profile'
                ? 'Como as pessoas veem você no Discordo.'
                : section === 'account'
                  ? 'Sua conta e a sessão neste aparelho.'
                  : section === 'appearance'
                    ? 'Tema, modo de cor e efeitos.'
                    : 'Dispositivos e processamento do microfone.'}
            </DialogDescription>

            {section === 'profile' ? <ProfileSection me={me} /> : null}
            {section === 'account' ? <AccountSection me={me} /> : null}
            {section === 'appearance' ? <AppearanceSettings /> : null}
            {section === 'voice' ? <VoiceVideoSettings active={open} /> : null}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** Open state for the settings, remembering the section. */
export const useSettingsState = () => {
  const [open, setOpen] = useState(false)
  const [section, setSection] = useState<SettingsSection>('profile')

  // Stable: it goes into the profile viewer's context value.
  const openAt = useCallback((at: SettingsSection) => {
    setSection(at)
    setOpen(true)
  }, [])

  return { open, section, setSection, setOpen, openAt }
}
