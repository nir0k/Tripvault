<script setup lang="ts">
import { useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { useLanguages } from '@/composables/useLanguages'
import { useSessionStore } from '@/stores/session'

// Choosing the interface language in a window of its own rather than from a
// dropdown inside a menu: the languages the reader is likely to want come
// first - the one in use and the one the browser asks for - and the rest follow
// underneath. Only languages the build carries a dictionary for are offered,
// because any other would leave the interface in English without saying why.
const { t, locale } = useI18n()
const session = useSessionStore()
const { recommended, rest } = useLanguages()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')

/** open shows the languages. */
function open(): void {
  dialog.value?.showModal()
}

// choose applies a language and closes the window. Saving it to the profile may
// fail without taking the choice away from this browser, which is where it is
// kept for the pages that have no account to read it from.
function choose(code: string): void {
  void session.setLocale(code).catch(() => {})
  dialog.value?.close()
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <div class="modal-box flex max-h-[85dvh] flex-col gap-3">
      <h2 class="text-lg font-bold">{{ t('preferences.languageTitle') }}</h2>

      <div class="min-h-0 flex-1 space-y-4 overflow-y-auto">
        <section class="space-y-1">
          <h3 class="px-1 text-sm font-semibold text-base-content/70">{{ t('preferences.languageRecommended') }}</h3>
          <ul>
            <li v-for="language in recommended" :key="language.code">
              <button
                type="button"
                class="btn btn-ghost w-full justify-start"
                :class="{ 'btn-active': language.code === locale }"
                @click="choose(language.code)"
              >
                <span class="flex-1 text-start">{{ language.name }}</span>
                <AppIcon v-if="language.code === locale" name="check" />
              </button>
            </li>
          </ul>
        </section>

        <section v-if="rest.length > 0" class="space-y-1">
          <h3 class="px-1 text-sm font-semibold text-base-content/70">{{ t('preferences.languageAll') }}</h3>
          <ul>
            <li v-for="language in rest" :key="language.code">
              <button
                type="button"
                class="btn btn-ghost w-full justify-start"
                :class="{ 'btn-active': language.code === locale }"
                @click="choose(language.code)"
              >
                <span class="flex-1 text-start">{{ language.name }}</span>
                <AppIcon v-if="language.code === locale" name="check" />
              </button>
            </li>
          </ul>
        </section>
      </div>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="dialog?.close()">{{ t('common.close') }}</button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
