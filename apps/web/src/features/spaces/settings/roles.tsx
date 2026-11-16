// Server settings tab: roles, their permissions and order.
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { ArrowDown, ArrowUp, Plus, Trash2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { filled } from '@/lib/forms'
import { has } from '@/lib/permissions'
import { cn } from '@/lib/utils'
import type { Guild, Role } from '@/lib/wire'
import { permissionList } from '../logic'

const toHex = (color: number) => `#${color.toString(16).padStart(6, '0')}`

export const roleColor = (r: Role) => (r.color ? toHex(r.color) : undefined)

type RoleForm = { name: string; color: number }

const RoleEditor = ({
  guild,
  role,
  editable,
  above,
  below,
  onDeleted,
}: {
  guild: Guild
  role: Role
  editable: boolean
  above: Role | undefined
  below: Role | undefined
  onDeleted: () => void
}) => {
  const everyone = role.id === guild.id
  const [confirming, setConfirming] = useState(false)

  const { register, handleSubmit, watch, setValue, reset, formState } = useForm<RoleForm>({
    defaultValues: { name: role.name, color: role.color },
    mode: 'onChange',
  })
  const color = watch('color')

  const save = useMutation({
    mutationFn: ({ name, color }: RoleForm) =>
      api.updateRole(guild.id, role.id, everyone ? { color } : { name: name.trim(), color }),
    onSuccess: (_, values) => reset(values),
  })

  // Each switch saves at once, apart from the name and color form.
  const setPermissions = useMutation({
    mutationFn: (permissions: number) => api.updateRole(guild.id, role.id, { permissions }),
  })

  const move = useMutation({ mutationFn: (up: boolean) => api.moveRole(guild.id, role.id, up) })

  const remove = useMutation({ mutationFn: () => api.deleteRole(guild.id, role.id), onSuccess: onDeleted })

  const error = save.error ?? setPermissions.error ?? move.error ?? remove.error

  return (
    <div className="min-w-0 space-y-4">
      {!editable ? (
        <p className="rounded-md bg-surface-raised px-3 py-2 text-xs text-greyple">
          Este cargo está na sua posição ou acima dela: você não pode editá-lo.
        </p>
      ) : null}
      <form onSubmit={handleSubmit((values) => save.mutate(values))} className="flex flex-wrap items-end gap-2">
        {everyone ? null : (
          <div className="min-w-40 flex-1">
            <label htmlFor="role-name" className="section-label">
              Nome
            </label>
            <Input id="role-name" className="mt-2" maxLength={100} disabled={!editable} {...register('name', filled)} />
          </div>
        )}

        <div>
          <label htmlFor="role-color" className="section-label">
            Cor
          </label>

          <div className="mt-2 flex items-center gap-1">
            <input
              id="role-color"
              type="color"
              value={toHex(color)}
              disabled={!editable}
              onChange={(e) => setValue('color', parseInt(e.target.value.slice(1), 16), { shouldDirty: true })}
              className="h-9 w-12 cursor-pointer rounded-md border border-white/10 bg-surface-raised disabled:opacity-45"
            />

            {color ? (
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                aria-label="Sem cor"
                title="Sem cor"
                disabled={!editable}
                onClick={() => setValue('color', 0, { shouldDirty: true })}
              >
                <X aria-hidden />
              </Button>
            ) : null}
          </div>
        </div>

        <Button type="submit" disabled={!editable || !formState.isDirty || !formState.isValid} loading={save.isPending}>
          Salvar
        </Button>
      </form>

      {everyone ? null : (
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            size="sm"
            variant="secondary"
            disabled={!editable || !above || move.isPending}
            onClick={() => move.mutate(true)}
          >
            <ArrowUp aria-hidden />
            Subir
          </Button>
          <Button
            type="button"
            size="sm"
            variant="secondary"
            disabled={!editable || !below || move.isPending}
            onClick={() => move.mutate(false)}
          >
            <ArrowDown aria-hidden />
            Descer
          </Button>
          <span className="flex-1" />
          {confirming ? (
            <Button
              type="button"
              size="sm"
              variant="destructive"
              autoFocus
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              {remove.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}Confirmar exclusão
            </Button>
          ) : (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="text-destructive hover:bg-destructive/15"
              disabled={!editable}
              onClick={() => setConfirming(true)}
            >
              <Trash2 aria-hidden />
              Excluir cargo
            </Button>
          )}
        </div>
      )}

      <FieldError error={error} />

      <div>
        <span className="section-label">Permissões</span>
        <ul className="mt-2 space-y-1">
          {permissionList().map(([bit, label]) => (
            <li key={bit} className="flex items-center gap-3 rounded-md bg-surface-raised px-3 py-2">
              <label htmlFor={`perm-${bit}`} className="min-w-0 flex-1 text-sm text-fog">
                {label}
              </label>
              <Switch
                id={`perm-${bit}`}
                checked={has(role.permissions, bit)}
                // Nobody grants or revokes what they do not hold.
                disabled={!editable || setPermissions.isPending || !has(guild.permissions, bit)}
                onCheckedChange={(on) => setPermissions.mutate(on ? role.permissions | bit : role.permissions & ~bit)}
              />
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

export const Roles = ({ guild, myTop }: { guild: Guild; myTop: number }) => {
  const sorted = guild.roles // the server sends them highest first
  const [selectedId, setSelectedId] = useState(guild.id)
  const create = useMutation({ mutationFn: () => api.createRole(guild.id, {}), onSuccess: (r) => setSelectedId(r.id) })
  const role = sorted.find((r) => r.id === selectedId) ?? sorted.find((r) => r.id === guild.id)
  const movable = sorted.filter((r) => r.id !== guild.id)
  const i = role ? movable.indexOf(role) : -1

  return (
    <div className="grid gap-4 sm:grid-cols-[180px_1fr]">
      <div className="space-y-2">
        <Button type="button" size="sm" className="w-full" onClick={() => create.mutate()} disabled={create.isPending}>
          {create.isPending ? <Spinner aria-hidden /> : <Plus aria-hidden />}Criar cargo
        </Button>
        <FieldError error={create.error} />
        <ul className="space-y-0.5">
          {sorted.map((r) => (
            <li key={r.id}>
              <button
                type="button"
                onClick={() => setSelectedId(r.id)}
                className={cn(
                  'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-greyple hover:bg-surface-hover hover:text-snow',
                  r.id === role?.id && 'bg-surface-active text-snow',
                )}
              >
                <span
                  aria-hidden
                  className="size-2.5 shrink-0 rounded-full bg-greyple"
                  style={{ backgroundColor: roleColor(r) }}
                />
                <span className="min-w-0 flex-1 truncate">{r.id === guild.id ? '@everyone' : r.name}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
      {role ? (
        <RoleEditor
          key={role.id}
          guild={guild}
          role={role}
          editable={role.position < myTop}
          above={i > 0 && movable[i - 1]!.position < myTop ? movable[i - 1] : undefined}
          below={i >= 0 ? movable[i + 1] : undefined}
          onDeleted={() => setSelectedId(guild.id)}
        />
      ) : null}
    </div>
  )
}
