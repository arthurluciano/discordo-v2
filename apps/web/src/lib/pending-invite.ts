// An invite link is /invite/CODE. Logging in goes through Discord and comes back to "/", so the
// code waits in sessionStorage (this tab only) until there is a session to use it with.
const KEY = 'discordo-pending-invite'
const PATH = /^\/invite\/([\w-]+)\/?$/

export const inviteLink = (code: string) => `${window.location.origin}/invite/${code}`

/** Takes the invite from the address bar (cleaning it up) or from an earlier visit. Call once. */
export const takePendingInvite = (): string | null => {
  const fromPath = PATH.exec(window.location.pathname)?.[1]
  if (fromPath) {
    window.history.replaceState(null, '', '/')
    try {
      sessionStorage.setItem(KEY, fromPath)
    } catch {
      // storage blocked: the invite still works right now, just not across the login redirect
    }
    return fromPath
  }
  try {
    return sessionStorage.getItem(KEY)
  } catch {
    return null
  }
}

export const clearPendingInvite = () => {
  try {
    sessionStorage.removeItem(KEY)
  } catch {
    // nothing stored
  }
}
