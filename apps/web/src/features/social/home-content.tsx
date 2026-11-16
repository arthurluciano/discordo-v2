// CONTRACT (implemented by the social area): the friends screen when dmId is null, else the DM
// (its ChannelChat, a call button, and the VoiceStage while in that DM's call).
export type HomeContentProps = { dmId: string | null; onBack: () => void }

export const HomeContent = ({ dmId }: HomeContentProps) => <div className="grid flex-1 place-items-center text-fog">{dmId ?? 'Amigos'}</div>
