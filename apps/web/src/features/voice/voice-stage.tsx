// CONTRACT (implemented by the voice area): the call view of a voice channel or DM.
export type VoiceStageProps = {
  channelId: string
  /** Mobile: back to the navigation pane. */
  onBack: () => void
}

export const VoiceStage = ({ channelId }: VoiceStageProps) => <div className="grid flex-1 place-items-center text-fog">Chamada {channelId}</div>
