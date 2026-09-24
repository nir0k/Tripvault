import { http } from './client'
import type { ClientConfig } from './types'

let loading: Promise<ClientConfig> | null = null

/**
 * getClientConfig reads the instance settings once per page load; they change
 * only when the server is reconfigured and restarted.
 */
export function getClientConfig(): Promise<ClientConfig> {
  if (!loading) {
    loading = http.get<ClientConfig>('/api/v1/config').then((response) => response.data)
    loading.catch(() => {
      loading = null
    })
  }
  return loading
}
