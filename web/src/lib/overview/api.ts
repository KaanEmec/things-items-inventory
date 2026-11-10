import { api } from '../api'
import type { Overview } from './types'

export const getOverview = () => api.get<Overview>('/api/v1/overview')
