<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { MEDIA_FAVORITE_LIMIT } from '@/api/media'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import JustifiedGrid from '@/components/media/JustifiedGrid.vue'
import MediaImage from '@/components/media/MediaImage.vue'
import MediaViewer from '@/components/media/MediaViewer.vue'
import { useDropdownGroup } from '@/composables/useDropdown'
import { tripRoute } from '@/utils/tripRoutes'

// The pictures of a day or a place, in rows of tiles in their own proportions
// that open full size. A day can hold far more than a report wants to show at once, so only
// a single row is laid out - as many as fit the width - and its last cell, showing
// the next picture, leads to the whole set; the viewer, however, always walks
// every picture of the gallery.
//
// While the report is being edited each tile also carries what can be done with
// it: the cover, whether it is private, which pictures the report
// shows, and taking it out of the gallery or off the service altogether.
//
// The pictures come in the order they were taken, which the server keeps; there
// is no order of one's own to drag them into.
//
// A day or a place may hold far more pictures than a report wants to print, so
// the report shows the favourites it was given, as many of them as fit - and
// while it was given none, the first of the gallery, because a report written
// before anybody chose is still a report with photographs in it.
const props = withDefaults(defineProps<{
  items: Media[]
  canEdit?: boolean
  /** The picture this day or place is shown by, out of these. */
  coverId?: string | null
  /** The trip whose gallery the last cell leads to; without it the cell is quiet. */
  tripId?: string
  /** Lay out the pictures the report shows rather than the first of the gallery. */
  preferFavorites?: boolean
  /** Whether the menu offers to choose what the report shows. */
  canFavorite?: boolean
}>(), {
  canEdit: false,
  coverId: null,
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

// strip is what the row is laid out from: the favourites the report shows
// first when it is being read that way, then the rest of the gallery, so the
// cell leading to the rest shows the next picture. Only the favourites may
// take a tile of the row while there are any.
const strip = computed(() => {
  if (props.preferFavorites && favorites.value.length > 0) {
    const chosen = favorites.value.slice(0, MEDIA_FAVORITE_LIMIT)
    return { items: [...chosen, ...props.items.filter((item) => !chosen.includes(item))], limit: chosen.length }
  }
  return { items: props.items, limit: Infinity }
})

// full says whether the report already shows as many pictures here as it has
// room for, which is when it stops offering to add another.
const full = computed(() => favorites.value.length >= MEDIA_FAVORITE_LIMIT)
</script>

<template>
  <div class="space-y-2">
    <JustifiedGrid
      :items="strip.items"
      :item-key="(item) => item.id"
      :dimensions="(item) => item"
      :row-height="160"
      strip
      :strip-limit="strip.limit"
    >
      <template #default="{ item, size }">
        <div class="group size-full">
          <!-- The viewer walks every picture of the gallery, not only those laid out. -->
          <button
            type="button"
            class="block size-full overflow-hidden"
            :aria-label="item.original_name"
            @click="viewer?.open(props.items.indexOf(item))"
          >
            <MediaImage :id="item.id" :alt="item.original_name" :size="size" fill />
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
        </div>
      </template>

      <!-- The way to the rest of the pictures, in the same row as the tiles and
           showing the next of them: the photos page of the report this gallery
           belongs to. -->
      <template #more="{ item, hidden, size }">
        <component
          :is="tripId ? RouterLink : 'span'"
          :to="tripId ? tripRoute({ id: tripId, kind: 'report' }, 'media') : undefined"
          class="relative block size-full overflow-hidden text-sm"
        >
          <MediaImage :id="item.id" alt="" :size="size" fill />
          <span class="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-black/55 text-white">
            <span class="text-lg font-semibold">+{{ hidden }}</span>
            <span class="px-1 text-center text-xs opacity-80">{{ t('media.showAll') }}</span>
          </span>
        </component>
      </template>
    </JustifiedGrid>

    <MediaViewer ref="viewer" :items="props.items" />
  </div>
</template>
