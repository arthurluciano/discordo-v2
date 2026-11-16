// CONTRACT (implemented by the servers area): a guild's header (menu, settings, invites) and channel
// list, with who is in each voice channel. Selecting a channel calls navigate.channel; a voice
// channel also joins its call (useVoice().join).
export type GuildSidebarProps = { guildId: string; channelId: string | null }

export const GuildSidebar = ({ guildId }: GuildSidebarProps) => <div className="p-4 text-fog">{guildId}</div>
