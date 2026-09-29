import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createTag, deleteTag, listTags, updateTag, type TagChanges } from '@/api/tags'
import type { Tag } from '@/api/types'
import { useTripStore } from '@/stores/trip'

/**
 * useTagsStore holds the reader's own tags, shared by the window that manages
 * them and the menu that puts them on a trip. A change made in one is mirrored
 * on the open trip, so its page does not show a name that no longer exists.
 */
export const useTagsStore = defineStore('tags', () => {
  const tags = ref<Tag[]>([])
  const loaded = ref(false)

  // sortTags keeps the list in the order the server gives it: by name.
  function sortTags(): void {
    tags.value.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
  }

  /** load reads the list, once unless asked again. */
  async function load(force = false): Promise<void> {
    if (loaded.value && !force) {
      return
    }
    tags.value = await listTags()
    loaded.value = true
  }

  /** create adds a tag and returns it. */
  async function create(name: string): Promise<Tag> {
    const tag = await createTag(name)
    tags.value.push(tag)
    sortTags()
    return tag
  }

  /** update renames or recolours a tag here and on the open trip. */
  async function update(id: string, changes: TagChanges): Promise<void> {
    const changed = await updateTag(id, changes)
    tags.value = tags.value.map((tag) => (tag.id === id ? changed : tag))
    sortTags()
    const trip = useTripStore().trip
    if (trip) {
      trip.tags = trip.tags
        .map((tag) => (tag.id === id ? { id, name: changed.name, color: changed.color } : tag))
        .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
    }
  }

  /** remove deletes a tag here and takes it off the open trip. */
  async function remove(id: string): Promise<void> {
    await deleteTag(id)
    tags.value = tags.value.filter((tag) => tag.id !== id)
    const trip = useTripStore().trip
    if (trip) {
      trip.tags = trip.tags.filter((tag) => tag.id !== id)
    }
  }

  /**
   * countOn moves the number of trips or ideas a tag is on after it was put on
   * one or taken off it.
   */
  function countOn(id: string, delta: number, kind: 'trip' | 'idea' = 'trip'): void {
    const tag = tags.value.find((item) => item.id === id)
    if (tag) {
      if (kind === 'idea') {
        tag.idea_count += delta
      } else {
        tag.trip_count += delta
      }
    }
  }

  /** reset forgets the list, for the next person to sign in. */
  function reset(): void {
    tags.value = []
    loaded.value = false
  }

  return { tags, loaded, load, create, update, remove, countOn, reset }
})
