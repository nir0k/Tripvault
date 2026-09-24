<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ActivityType, PlaceCategory } from '@/api/types'
import IconSelect from '@/components/IconSelect.vue'
import { activityTypeOptions, placeCategoryOptions } from '@/utils/plan'

// What an element of a day is: a place with its category, or an activity with
// its type. Both forms share it, so a place can become an activity later - a
// waterfall planned as a sight may turn out to be the start of a canyon.
const kind = defineModel<'place' | 'activity'>('kind', { required: true })
const category = defineModel<PlaceCategory>('category', { required: true })
const activityType = defineModel<ActivityType>('activityType', { required: true })

const { t } = useI18n()

const categories = computed(() => placeCategoryOptions(t))
const activities = computed(() => activityTypeOptions(t))
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="join" role="radiogroup" :aria-label="t('place.kind')">
      <input
        v-model="kind"
        type="radio"
        value="place"
        class="btn btn-sm join-item"
        :aria-label="t('place.kinds.place')"
      />
      <input
        v-model="kind"
        type="radio"
        value="activity"
        class="btn btn-sm join-item"
        :aria-label="t('place.kinds.activity')"
      />
    </div>
    <label class="flex flex-col gap-1">
      <template v-if="kind === 'activity'">
        <span class="label">{{ t('place.activityType') }}</span>
        <IconSelect v-model="activityType" :options="activities" :label="t('place.activityType')" block />
      </template>
      <template v-else>
        <span class="label">{{ t('place.category') }}</span>
        <IconSelect v-model="category" :options="categories" :label="t('place.category')" block />
      </template>
    </label>
  </div>
</template>
