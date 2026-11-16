import { useEffect, useState } from 'react'

/** Copies text to the clipboard; `copied` stays true for two seconds after it worked. */
export const useCopy = () => {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 2000)
    return () => clearTimeout(timer)
  }, [copied])
  const copy = (text: string) => navigator.clipboard.writeText(text).then(() => setCopied(true), () => {})
  return { copied, copy }
}
