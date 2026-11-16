// Pure helpers for the servers area: invite parsing, overwrite tri-state, labels, labels.
import { Permission, type PermissionName } from '@/lib/permissions'
import type { Overwrite } from '@/lib/wire'

/** Accepts a bare code or any link ending in /invite/CODE. Null when it is neither. */
export const parseInviteCode = (input: string): string | null => {
  const text = input.trim()
  const fromLink = /\/invite\/([\w-]+)\/?(?:[?#].*)?$/.exec(text)?.[1]
  if (fromLink) return fromLink
  return /^[\w-]+$/.test(text) ? text : null
}

export const INVITE_EXPIRY = [
  { label: 'Nunca', seconds: 0 },
  { label: '30 minutos', seconds: 30 * 60 },
  { label: '1 hora', seconds: 60 * 60 },
  { label: '1 dia', seconds: 24 * 60 * 60 },
  { label: '7 dias', seconds: 7 * 24 * 60 * 60 },
] as const

export type TriState = 'allow' | 'inherit' | 'deny'

export const bitState = (o: Pick<Overwrite, 'allow' | 'deny'> | undefined, bit: number): TriState =>
  !o ? 'inherit' : o.allow & bit ? 'allow' : o.deny & bit ? 'deny' : 'inherit'

export const setBitState = (o: Pick<Overwrite, 'allow' | 'deny'> | undefined, bit: number, state: TriState) => {
  const allow = (o?.allow ?? 0) & ~bit
  const deny = (o?.deny ?? 0) & ~bit
  return { allow: state === 'allow' ? allow | bit : allow, deny: state === 'deny' ? deny | bit : deny }
}

export const PERMISSION_LABELS: Record<PermissionName, string> = {
  ViewChannel: 'Ver canal',
  SendMessages: 'Enviar mensagens',
  ManageMessages: 'Gerenciar mensagens',
  AttachFiles: 'Anexar arquivos',
  AddReactions: 'Adicionar reações',
  MentionEveryone: 'Mencionar @everyone',
  Connect: 'Conectar (voz)',
  Speak: 'Falar (voz)',
  Stream: 'Transmitir vídeo',
  CreateInvite: 'Criar convite',
  KickMembers: 'Expulsar membros',
  BanMembers: 'Banir membros',
  ManageChannels: 'Gerenciar canais',
  ManageRoles: 'Gerenciar cargos',
  ManageGuild: 'Gerenciar servidor',
  Administrator: 'Administrador',
}

/** [bit, label] for every permission, optionally limited to a mask. */
export const permissionList = (mask = -1) =>
  (Object.keys(Permission) as PermissionName[])
    .filter((name) => Permission[name] & mask)
    .map((name) => [Permission[name], PERMISSION_LABELS[name]] as const)
