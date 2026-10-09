import { onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { searchPlaces } from '@/api/geo'
import type { GeoPlace } from '@/api/types'

/** useLocationSearch debounces place queries and discards superseded or disposed answers. */
export function useLocationSearch(
  query: Ref<string>,
  enabled: () => boolean,
  focus: () => { lat: number; lng: number } | null,
  onError: (error: unknown) => void,
  skip: (text: string) => boolean = () => false,
): { results: Ref<GeoPlace[]>; searching: Ref<boolean>; searched: Ref<boolean> } {
  const { locale } = useI18n()
  const results = ref<GeoPlace[]>([])
  const searching = ref(false)
  const searched = ref(false)
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  watch(query, (text) => {
    clearTimeout(timer)
    const current = ++generation
    results.value = []
    searched.value = false
    const trimmed = text.trim()
    searching.value = trimmed.length >= 2 && enabled() && !skip(trimmed)
    if (!searching.value) return
    timer = setTimeout(async () => {
      try {
        const found = await searchPlaces(trimmed, locale.value, focus())
        if (generation === current) results.value = found
      } catch (err) {
        if (generation === current) onError(err)
      } finally {
        if (generation === current) { searching.value = false; searched.value = true }
      }
    }, 350)
  })
  onBeforeUnmount(() => { generation++; clearTimeout(timer) })
  return { results, searching, searched }
}
