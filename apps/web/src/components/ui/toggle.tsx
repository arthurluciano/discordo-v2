import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { Toggle as TogglePrimitive } from 'radix-ui'
import { cn } from '@/lib/utils'

const toggleVariants = cva("group/toggle inline-flex items-center justify-center gap-1 rounded-lg border border-transparent text-sm font-semibold whitespace-nowrap transition-[transform,background-color,color] outline-none hover:bg-surface-hover focus-visible:ring-2 focus-visible:ring-ring/60 active:scale-95 disabled:pointer-events-none disabled:opacity-45 data-[state=on]:bg-blurple data-[state=on]:text-white [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4", {
  variants: {
    variant: { default: 'bg-surface-raised text-fog', outline: 'border-border bg-transparent text-fog' },
    size: { default: 'h-9 min-w-9 px-3 max-md:h-11 max-md:min-w-11', sm: 'h-8 min-w-8 px-2.5 text-xs max-md:h-10 max-md:min-w-10 [&_svg:not([class*=size-])]:size-3.5', lg: 'h-11 min-w-11 px-4 max-md:h-12 max-md:min-w-12' },
  },
  defaultVariants: { variant: 'default', size: 'default' },
})

function Toggle({ className, variant = 'default', size = 'default', ...props }: React.ComponentProps<typeof TogglePrimitive.Root> & VariantProps<typeof toggleVariants>) {
  return <TogglePrimitive.Root data-slot="toggle" className={cn(toggleVariants({ variant, size, className }))} {...props} />
}

export { Toggle, toggleVariants }
