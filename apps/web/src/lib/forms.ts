// Shared react-hook-form rules.

/** A text field that must have something besides spaces. */
export const filled = { validate: (value: string) => value.trim() !== '' }
