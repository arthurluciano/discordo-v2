import * as React from 'react'
import { cn } from '@/lib/utils'

function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return <input type={type} data-slot="input" className={cn('h-10 w-full min-w-0 rounded-lg border border-transparent bg-surface-input px-3 py-2 text-base text-foreground transition-[border-color,box-shadow] outline-none placeholder:text-greyple focus-visible:border-blurple-soft focus-visible:ring-2 focus-visible:ring-blurple/25 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/20 md:text-sm', className)} {...props} />
}

export { Input }
