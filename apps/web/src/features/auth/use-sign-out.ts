import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { stopGateway } from '@/lib/gateway'
import { reset } from '@/lib/store'

export const useSignOut = () =>
  useMutation({
    mutationFn: api.logout,
    onSuccess: () => {
      stopGateway()
      reset('signed-out')
    },
  })
