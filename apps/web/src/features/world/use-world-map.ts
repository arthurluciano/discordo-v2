import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

/** The map is the same for every guild and only changes with the server: fetched once. */
export const useWorldMap = () => useQuery({ queryKey: ['world-map'], queryFn: api.worldMap, staleTime: Infinity }).data
