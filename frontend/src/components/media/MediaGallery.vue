<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { MEDIA_FAVORITE_LIMIT } from '@/api/media'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaImage from '@/components/media/MediaImage.vue'
import MediaViewer from '@/components/media/MediaViewer.vue'
import { useDropdownGroup } from '@/composables/useDropdown'
import { tripRoute } from '@/utils/tripRoutes'

// The pictures of a day or a place, as a row of equal tiles that open full
// size. A day can hold far more than a report wants to show at once, so only
// the first few are laid out and the last cell leads to the whole set; the
// viewer, however, always walks every picture of the gallery.
//
// While the report is being edited each tile also carries what can be done with
// it: the cover, whether it is private, which pictures the report
// shows, and taking it out of the gallery or off the service altogether.
//
// The pictures come in the order they were taken, which the server keeps; there
// is no order of one's own to drag them into.
//
// A day or a place may hold far more pictures than a report wants to print, so
// the report shows the favourites it was given - and while it was given none,
// the first few, because a report written before anybody chose is still a
// report with photographs in it.
const props = withDefaults(defineProps<{
  items: Media[]
  canEdit?: boolean
  /** The picture this day or place is shown by, out of these. */
  coverId?: string | null
  /** How many tiles are laid out before the last cell leads to the rest. */
  limit?: number
  /** The trip whose gallery the last cell leads to; without it the cell is quiet. */
  tripId?: string
  /** Lay out the pictures the report shows rather than the first of the gallery. */
  preferFavorites?: boolean
  /** Whether the menu offers to choose what the report shows. */
  canFavorite?: boolean
}>(), {
  canEdit: false,
  coverId: null,
  limit: MEDIA_FAVORITE_LIMIT,
  tripId: '',
  preferFavorites: false,
  canFavorite: false,
})

const emit = defineEmits<{
  cover: [media: Media | null]
  privacy: [media: Media, isPrivate: boolean]
  favorite: [media: Media, isFavorite: boolean]
  unlink: [media: Media]
  remove: [media: Media]
}>()

const { t } = useI18n()
const { close: closeMenu } = useDropdownGroup()
const viewer = useTemplateRef<InstanceType<typeof MediaViewer>>('viewer')

const favorites = computed(() => props.items.filter((item) => item.is_favorite))

// shown are the tiles laid out: what the report shows when it is being read
// that way, and otherwise the first of the gallery.
const shown = computed(() => {
  if (props.preferFavorites && favorites.value.length > 0) {
    return favorites.value.slice(0, props.limit)
  }
  return props.items.slice(0, props.limit)
})
const hidden = computed(() => Math.max(props.items.length - shown.value.length, 0))

// full says whether the report already shows as many pictures here as it has
// room for, which is when it stops offering to add another.
const full = computed(() => favorites.value.length >= MEDIA_FAVORITE_LIMIT)
</script>

<template>
  <div class="space-y-2">
    <ul class="grid grid-cols-4 gap-2 sm:grid-cols-6 lg:grid-cols-8">
      <li v-for="(item, index) in shown" :key="item.id" class="group relative">
        <button
          type="button"
          class="block w-full overflow-hidden rounded-box border border-base-300"
          :aria-label="item.original_name"
          @click="viewer?.open(index)"
        >
          <MediaImage :id="item.id" :alt="item.original_name" :size="320" square />
        </button>

        <span class="pointer-events-none absolute start-1 top-1 flex items-center gap-1">
          <span v-if="item.id === coverId" class="badge badge-primary badge-xs">{{ t('media.cover') }}</span>
          <AppIcon
            v-if="item.is_favorite"
            name="starFill"
            class="size-4! text-amber-400 drop-shadow-[0_1px_1px_rgb(0_0_0/0.7)]"
            :aria-label="t('media.favorite')"
            role="img"
          />
          <AppIcon
            v-if="item.is_private"
            name="lockFill"
            class="size-4! text-amber-400 drop-shadow-[0_1px_1px_rgb(0_0_0/0.7)]"
            :aria-label="t('media.private')"
            role="img"
          />
        </span>

        <details v-if="canEdit" class="dropdown dropdown-end absolute end-1 top-1">
          <summary class="btn btn-square btn-xs" :aria-label="t('media.actions')">
            <AppIcon name="dots" />
          </summary>
          <ul
            class="menu dropdown-content z-20 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg"
            @click="closeMenu"
          >
            <li>
              <button type="button" @click="emit('cover', item.id === coverId ? null : item)">
                <AppIcon name="image" />
                {{ item.id === coverId ? t('media.unsetCover') : t('media.setCover') }}
              </button>
            </li>
            <li>
              <button type="button" @click="emit('privacy', item, !item.is_private)">
                <AppIcon :name="item.is_private ? 'eye' : 'eyeSlash'" />
                {{ item.is_private ? t('media.makePublic') : t('media.makePrivate') }}
              </button>
            </li>
            <li v-if="canFavorite">
              <button
                type="button"
                :disabled="full && !item.is_favorite"
                :title="full && !item.is_favorite ? t('media.favoriteFull') : undefined"
                @click="emit('favorite', item, !item.is_favorite)"
              >
                <AppIcon :name="item.is_favorite ? 'star' : 'starFill'" />
                {{ item.is_favorite ? t('media.unsetFavorite') : t('media.setFavorite') }}
              </button>
            </li>
            <li>
              <button type="button" @click="emit('unlink', item)">
                <AppIcon name="collapse" />
                {{ t('media.unlink') }}
              </button>
            </li>
            <li>
              <button type="button" class="text-error" @click="emit('remove', item)">
                <AppIcon name="trash" />
                {{ t('media.remove') }}
              </button>
            </li>
          </ul>
        </details>
      </li>

      <!-- The way to the rest of the pictures, in the same row as the tiles: the
           photos page of the report this gallery belongs to. -->
      <li v-if="hidden > 0">
        <component
          :is="tripId ? RouterLink : 'span'"
          :to="tripId ? tripRoute({ id: tripId, kind: 'report' }, 'media') : undefined"
          class="flex aspect-square w-full flex-col items-center justify-center gap-1 rounded-box border border-base-300 bg-base-200 text-sm"
        >
          <span class="text-lg font-semibold">+{{ hidden }}</span>
          <span class="px-1 text-center text-xs opacity-70">{{ t('media.showAll') }}</span>
        </component>
      </li>
    </ul>

    <MediaViewer ref="viewer" :items="props.items" />
  </div>
</template>
