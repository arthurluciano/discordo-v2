import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { stopGateway } from '@/lib/gateway'
import { setState } from '@/lib/store'

export const useSignOut = () =>
  useMutation({
    mutationFn: api.logout,
    onSuccess: () => {
      stopGateway()
      setState((s) => ({ ...s, status: 'signed-out' }))
    },
  })
