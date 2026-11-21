// Server settings tab: name, icon and banner.
import { useRef } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { ImagePlus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { api } from '@/lib/api'
import { filled } from '@/lib/forms'
import { cn } from '@/lib/utils'
import type { Guild } from '@/lib/wire'
import { initialsOf } from '../server-rail'

export const ImagePicker = ({
  label,
  url,
  wide,
  fallback,
  onChange,
}: {
  label: string
  url: string
  wide?: boolean
  fallback: string
  onChange: (url: string) => void
}) => {
  const input = useRef<HTMLInputElement>(null)
  const upload = useMutation({ mutationFn: (file: File) => api.uploadImage(file), onSuccess: onChange })

  return (
    <div className="space-y-2">
      <span className="section-label">{label}</span>
      <div className="flex items-center gap-3">
        <div
          className={cn(
            'grid shrink-0 place-items-center overflow-hidden bg-surface-raised text-lg font-bold text-fog',
            wide ? 'h-20 w-40 rounded-lg' : 'size-20 rounded-2xl',
          )}
        >
          {url ? <img src={url} alt="" className="size-full object-cover" /> : fallback}
        </div>
        <div className="flex flex-col gap-1.5">
          <Button
            type="button"
            size="sm"
            variant="secondary"
            onClick={() => input.current?.click()}
            disabled={upload.isPending}
          >
            {upload.isPending ? <Spinner aria-hidden /> : <ImagePlus aria-hidden />}Enviar imagem
          </Button>
          {url ? (
            <Button type="button" size="sm" variant="ghost" onClick={() => onChange('')}>
              <X aria-hidden />
              Remover
            </Button>
          ) : null}
        </div>
        <input
          ref={input}
          type="file"
          accept="image/png,image/jpeg,image/gif,image/webp"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) upload.mutate(file)
            e.target.value = ''
          }}
        />
      </div>
      <FieldError error={upload.error} />
    </div>
  )
}

type GuildForm = { name: string; icon_url: string; banner_url: string }

export const Overview = ({ guild }: { guild: Guild }) => {
  const { register, handleSubmit, watch, setValue, reset, formState } = useForm<GuildForm>({
    defaultValues: { name: guild.name, icon_url: guild.icon_url, banner_url: guild.banner_url },
    mode: 'onChange',
  })
  const [name, icon, banner] = watch(['name', 'icon_url', 'banner_url'])

  const save = useMutation({
    mutationFn: (values: GuildForm) => api.updateGuild(guild.id, { ...values, name: values.name.trim() }),
    onSuccess: (_, values) => reset(values),
  })

  return (
    <form onSubmit={handleSubmit((values) => save.mutate(values))} className="space-y-5">
      <div>
        <label htmlFor="guild-settings-name" className="section-label">
          Nome do servidor
        </label>
        <Input id="guild-settings-name" className="mt-2" maxLength={100} {...register('name', filled)} />
      </div>

      <ImagePicker
        label="Ícone"
        url={icon}
        fallback={initialsOf(name)}
        onChange={(url) => setValue('icon_url', url, { shouldDirty: true })}
      />

      <ImagePicker
        label="Banner"
        url={banner}
        wide
        fallback=""
        onChange={(url) => setValue('banner_url', url, { shouldDirty: true })}
      />

      <FieldError error={save.error} />

      <Button type="submit" disabled={!formState.isDirty || !formState.isValid} loading={save.isPending}>
        Salvar alterações
      </Button>
    </form>
  )
}
