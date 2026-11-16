import * as React from 'react'
import { cn } from '@/lib/utils'

function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return <textarea data-slot="textarea" className={cn('flex field-sizing-content min-h-20 w-full rounded-lg border border-transparent bg-surface-input px-3 py-2.5 text-base text-foreground transition-[border-color,box-shadow] outline-none placeholder:text-greyple focus-visible:border-blurple-soft focus-visible:ring-2 focus-visible:ring-blurple/25 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/20 md:text-sm', className)} {...props} />
}

export { Textarea }
