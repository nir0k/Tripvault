import { onScopeDispose } from 'vue'

/**
 * useLoadGeneration lets only the latest load update its owner's state.
 * Disposing the component or store also invalidates unfinished requests.
 */
export function useLoadGeneration(): () => () => boolean {
  let generation = 0
  onScopeDispose(() => { generation++ })
  return () => {
    const current = ++generation
    return () => current === generation
  }
}
