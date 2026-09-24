<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { listMedia } from '@/api/media'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaImage from '@/components/media/MediaImage.vue'
import MediaUploader from '@/components/media/MediaUploader.vue'
import { errorMessage } from '@/utils/errors'

// Choosing one of a trip's pictures, with the option of adding a new one on the
// spot: a cover is usually picked from what is already there, but the first one
// has to come from somewhere.
const props = defineProps<{ tripId: string }>()

const emit = defineEmits<{
  choose: [media: Media]
}>()

const { t, te } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const items = ref<Media[]>([])
const loading = ref(false)
const error = ref('')

/** open shows the trip's pictures, reading them afresh each time. */
async function open(): Promise<void> {
  dialog.value?.showModal()
  loading.value = true
  error.value = ''
  try {
    items.value = await listMedia(props.tripId)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loading.value = false
  }
}

/** close hides the picker. */
function close(): void {
  dialog.value?.close()
}

// choose hands the picture to the page and closes.
function choose(media: Media): void {
  emit('choose', media)
  close()
}

// onUploaded puts what was just uploaded at the top, ready to be picked.
function onUploaded(media: Media[]): void {
  items.value = [...media, ...items.value]
}

defineExpose({ open, close })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <div class="modal-box flex max-h-[85dvh] w-[min(96vw,56rem)] max-w-none flex-col gap-3">
      <h2 class="text-lg font-bold">{{ t('media.pickTitle') }}</h2>

      <MediaUploader :trip-id="props.tripId" @uploaded="onUploaded" />

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div v-else-if="loading" class="flex justify-center py-6"><span class="loading loading-spinner"></span></div>
      <p v-else-if="items.length === 0" class="py-4 text-sm text-base-content/70">{{ t('media.pickEmpty') }}</p>
      <ul v-else class="grid grid-cols-3 gap-2 overflow-y-auto sm:grid-cols-5 lg:grid-cols-6">
        <li v-for="item in items" :key="item.id" class="relative">
          <button
            type="button"
            class="block w-full overflow-hidden rounded-box border border-base-300"
            :aria-label="item.original_name"
            @click="choose(item)"
          >
            <MediaImage :id="item.id" :alt="item.original_name" :size="320" square />
          </button>
          <AppIcon
            v-if="item.is_private"
            name="lockFill"
            class="pointer-events-none absolute start-1 top-1 size-4! text-amber-400 drop-shadow-[0_1px_1px_rgb(0_0_0/0.7)]"
            role="img"
            :aria-label="t('media.private')"
          />
        </li>
      </ul>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">
          <AppIcon name="close" />
          {{ t('common.cancel') }}
        </button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
