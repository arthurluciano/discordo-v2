// Server settings tab: deleting the server (owner only).
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { api } from '@/lib/api'
import type { Guild } from '@/lib/wire'

export const DeleteGuild = ({ guild }: { guild: Guild }) => {
  const { register, handleSubmit, formState } = useForm({ defaultValues: { typed: '' }, mode: 'onChange' })

  // GUILD_DELETE then closes everything and the workspace goes home.
  const remove = useMutation({ mutationFn: () => api.deleteGuild(guild.id) })

  return (
    <form onSubmit={handleSubmit(() => remove.mutate())} className="space-y-4">
      <p className="text-sm text-fog">Isso apaga o servidor, todos os canais e mensagens. Não dá para desfazer.</p>

      <div>
        <label htmlFor="delete-guild-name" className="section-label">
          Digite <strong className="text-snow">{guild.name}</strong> para confirmar
        </label>
        <Input
          id="delete-guild-name"
          className="mt-2"
          autoComplete="off"
          {...register('typed', { validate: (typed) => typed === guild.name })}
        />
      </div>

      <FieldError error={remove.error} />

      <Button type="submit" variant="destructive" disabled={!formState.isValid || remove.isPending}>
        {remove.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}
        Excluir servidor
      </Button>
    </form>
  )
}
