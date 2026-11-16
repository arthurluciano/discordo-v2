import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { Slot } from 'radix-ui'
import { cn } from '@/lib/utils'
import { Spinner } from './spinner'

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-semibold whitespace-nowrap transition-[transform,background-color,color,border-color] duration-150 outline-none select-none focus-visible:ring-2 focus-visible:ring-ring/70 active:not-aria-[haspopup]:scale-[0.97] disabled:pointer-events-none disabled:opacity-45 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/25 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-blurple-hover',
        outline: 'border-border bg-surface-raised text-foreground hover:bg-surface-active',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-surface-active',
        ghost: 'bg-transparent text-muted-foreground hover:bg-surface-hover hover:text-foreground',
        destructive: 'bg-destructive text-white hover:bg-destructive/80',
        link: 'border-transparent bg-transparent px-0 text-cerulean underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 gap-2 px-4 max-md:h-11',
        xs: 'h-7 gap-1 rounded-md px-2.5 text-xs max-md:h-9 [&_svg:not([class*=size-])]:size-3',
        sm: 'h-8 gap-1.5 rounded-md px-3 text-xs max-md:h-10 [&_svg:not([class*=size-])]:size-3.5',
        lg: 'h-11 gap-2.5 rounded-xl px-5 max-md:h-12',
        icon: 'size-9 p-0 max-md:size-11',
        'icon-xs': 'size-7 rounded-md p-0 max-md:size-9 [&_svg:not([class*=size-])]:size-3',
        'icon-sm': 'size-8 rounded-md p-0 max-md:size-11 max-md:[&_svg:not([class*=size-])]:size-5',
        'icon-lg': 'size-11 rounded-xl p-0 max-md:size-12',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

/** `loading` shows a spinner before the label and disables the button. */
function Button({
  className,
  variant = 'default',
  size = 'default',
  asChild = false,
  loading = false,
  disabled,
  children,
  ...props
}: React.ComponentProps<'button'> & VariantProps<typeof buttonVariants> & { asChild?: boolean; loading?: boolean }) {
  const Comp = asChild ? Slot.Root : 'button'

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      disabled={disabled || loading}
      {...props}
    >
      {asChild ? (
        children
      ) : (
        <>
          {loading ? <Spinner aria-hidden /> : null}
          {children}
        </>
      )}
    </Comp>
  )
}

export { Button, buttonVariants }
