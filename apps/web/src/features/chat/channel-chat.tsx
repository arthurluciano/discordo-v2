// CONTRACT (implemented by the chat area): the conversation of a guild text channel or a DM.
import type { ReactNode } from 'react'

export type ChannelChatProps = {
  channelId: string
  /** Undefined for DMs. */
  guildId?: string
  title: string
  topic?: string
  /** Extra header buttons (e.g. start a call, group members). */
  actions?: ReactNode
  /** Mobile: back to the navigation pane. */
  onBack: () => void
}

export const ChannelChat = ({ title }: ChannelChatProps) => <div className="grid flex-1 place-items-center text-fog">{title}</div>
