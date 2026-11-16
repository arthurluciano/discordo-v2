// Server settings: overview (name, icon, banner), roles, members, invites, bans, and deleting the server.
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { has, Permission, topPosition } from '@/lib/permissions'
import { cn } from '@/lib/utils'
import type { Guild } from '@/lib/wire'
import { useMe } from '../identity/users'
import { Overview } from './settings/overview'
import { Roles } from './settings/roles'
import { Members } from './settings/members'
import { Bans, Invites } from './settings/invites'
import { DeleteGuild } from './settings/delete-guild'

/** The tabs this user may open; none means the settings are not offered at all. */
export const settingsSections = (guild: Guild, meId: string) => {
  const can = (bits: number) => has(guild.permissions, bits)
  return [
    { value: 'overview', label: 'Visão geral', show: can(Permission.ManageGuild) },
    { value: 'roles', label: 'Cargos', show: can(Permission.ManageRoles) },
    { value: 'members', label: 'Membros', show: can(Permission.ManageRoles) || can(Permission.KickMembers) || can(Permission.BanMembers) },
    { value: 'invites', label: 'Convites', show: can(Permission.ManageGuild) },
    { value: 'bans', label: 'Banimentos', show: can(Permission.BanMembers) },
    { value: 'delete', label: 'Excluir servidor', show: guild.owner_id === meId },
  ].filter((s) => s.show)
}

export const ServerSettingsDialog = ({ guild, onClose }: { guild: Guild; onClose: () => void }) => {
  const me = useMe()
  if (!me) return null
  const myTop = topPosition(guild, me.id)
  const sections = settingsSections(guild, me.id)

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto bg-surface-content sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Configurações do servidor</DialogTitle>
          <DialogDescription>{guild.name}</DialogDescription>
        </DialogHeader>
        {sections.length === 0 ? <p className="text-sm text-greyple">Você não tem permissão para mudar nada aqui.</p> : (
          <Tabs defaultValue={sections[0]!.value}>
            <TabsList className="max-w-full overflow-x-auto">
              {sections.map((s) => <TabsTrigger key={s.value} value={s.value} className={cn(s.value === 'delete' && 'text-destructive')}>{s.label}</TabsTrigger>)}
            </TabsList>
            <TabsContent value="overview" className="pt-2"><Overview guild={guild} /></TabsContent>
            <TabsContent value="roles" className="pt-2"><Roles guild={guild} myTop={myTop} /></TabsContent>
            <TabsContent value="members" className="pt-2"><Members guild={guild} myTop={myTop} meId={me.id} /></TabsContent>
            <TabsContent value="invites" className="pt-2"><Invites guild={guild} /></TabsContent>
            <TabsContent value="bans" className="pt-2"><Bans guild={guild} /></TabsContent>
            <TabsContent value="delete" className="pt-2"><DeleteGuild guild={guild} /></TabsContent>
          </Tabs>
        )}
      </DialogContent>
    </Dialog>
  )
}
