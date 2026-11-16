// CONTRACT (implemented by the social area): the home column: friends entry and the DM list
// (with creating a group DM). Selecting calls navigate.friends() or navigate.dm(id).
export const HomeSidebar = ({ dmId }: { dmId: string | null }) => <div className="p-4 text-fog">{dmId ?? 'Amigos'}</div>
