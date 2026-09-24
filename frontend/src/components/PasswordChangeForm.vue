<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { changePassword } from '@/api/me'
import { errorMessage } from '@/utils/errors'

const emit = defineEmits<{ changed: [] }>()

const { t, te } = useI18n()
const current = ref('')
const next = ref('')
const confirmation = ref('')
const error = ref('')
const busy = ref(false)

// submit checks the repeated password here - the server never sees the
// confirmation - and leaves every other rule to the API.
async function submit(): Promise<void> {
  error.value = ''
  if (next.value !== confirmation.value) {
    error.value = t('password.mismatch')
    return
  }
  busy.value = true
  try {
    await changePassword(current.value, next.value)
    current.value = ''
    next.value = ''
    confirmation.value = ''
    emit('changed')
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="flex flex-col gap-3" @submit.prevent="submit">
    <label class="floating-label">
      <span>{{ t('password.current') }}</span>
      <input v-model="current" type="password" autocomplete="current-password" required class="input w-full" :placeholder="t('password.current')" />
    </label>
    <label class="floating-label">
      <span>{{ t('password.new') }}</span>
      <input v-model="next" type="password" autocomplete="new-password" required minlength="8" class="input w-full" :placeholder="t('password.new')" />
    </label>
    <label class="floating-label">
      <span>{{ t('password.confirm') }}</span>
      <input v-model="confirmation" type="password" autocomplete="new-password" required class="input w-full" :placeholder="t('password.confirm')" />
    </label>
    <p class="text-sm text-base-content/70">{{ t('password.hint') }}</p>
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <button type="submit" class="btn btn-primary" :disabled="busy">
      <span v-if="busy" class="loading loading-spinner loading-sm"></span>
      {{ t('password.submit') }}
    </button>
  </form>
</template>
