// Wire types of the Tripvault API, as documented in the backend's OpenAPI file.

export type Theme = 'light' | 'dark' | 'auto'

/** Units is how far a distance is shown in; everything is carried in metres. */
export type Units = 'km' | 'mi'

/** DateFormat is whether the day or the month is written first. */
export type DateFormat = 'dmy' | 'mdy'

/** TimeFormat is the clock a time of day is read on. */
export type TimeFormat = 'h24' | 'h12'

export interface User {
  id: string
  email: string
  display_name: string
  is_admin: boolean
  is_active: boolean
  must_change_password: boolean
  /** Interface language; empty follows the browser. */
  locale: string
  theme: Theme
  /** How far this reader sees a distance in; everything is carried in metres. */
  units: Units
  /** Whether this reader sees the day or the month first. */
  date_format: DateFormat
  /** Whether this reader sees 14:00 or 2:00 PM. */
  time_format: TimeFormat
  default_currency: string
  /** Whether the account wears a picture; its bytes are fetched separately. */
  has_avatar: boolean
  /** When that picture last changed, which is what makes a browser fetch it again. */
  avatar_updated_at: string | null
  last_login_at: string | null
  created_at: string
}

export interface AdminUser extends User {
  /** The reader's own account. */
  is_self: boolean
}

export interface SessionResponse {
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number
  session_id: string
  user: User
}

export interface SessionItem {
  id: string
  user_agent: string
  created_at: string
  last_used_at: string
  expires_at: string
  current: boolean
}

export interface ClientConfig {
  version: string
  locales: string[]
  /** False when road legs are estimates because no routing provider is configured. */
  routing_enabled: boolean
  /** False when place search and reverse lookups are unavailable. */
  geocoding_enabled: boolean
  /** Raster tile URL template with {z}, {x} and {y}. */
  map_tile_url: string
  /** Attribution the map must show; may contain links. */
  map_attribution: string
}

/** ProviderStatus describes an external provider and its cache. */
export interface ProviderStatus {
  provider: string
  configured: boolean
  last_success_at: string | null
  last_error_at: string | null
  requests_24h: number
  errors_24h: number
  daily_limit: number
  cache_entries: number
  cache_hits: number
}

export interface ServiceStatus {
  version: string
  schema_version: number
  users: { total: number; active: number; admins: number }
  routing: ProviderStatus
  geocoding: ProviderStatus
}

/**
 * PreviewStatus is how the background rendering of photo previews is going.
 * Work comes in waves - a batch of uploads, the walk over stored files at
 * start-up or after a restore - and total, done and started_at describe the
 * current wave, or the last one while active is false. Every figure starts
 * again when the service restarts.
 */
export interface PreviewStatus {
  /** False when previews are rendered only when first asked for. */
  background: boolean
  active: boolean
  total: number
  done: number
  started_at: string | null
  /** Uploads waiting for their previews. */
  queued: number
  /** Files being decoded right now. */
  rendering: number
  backfill: { running: boolean; checked: number; total: number }
  /** Files rendered and files that could not be, since the service started. */
  rendered: number
  failed: number
}

/** GeoPlace is one place search or reverse lookup result. */
export interface GeoPlace {
  name: string
  /** The name with its region, for a list of choices. */
  label: string
  lat: number
  lng: number
  layer: string
  country: string
  /** The result's identifier at its source. */
  ref: string
}

export interface ListResponse<T> {
  items: T[]
}

/** PageResponse is one page of a paged list; next_cursor is null on the last. */
export interface PageResponse<T> {
  items: T[]
  next_cursor: string | null
}

export type TravelMode = 'walk' | 'car' | 'bike' | 'transit' | 'flight' | 'other'

export const TRAVEL_MODES: readonly TravelMode[] = ['walk', 'car', 'bike', 'transit', 'flight', 'other']

export type TripRole = 'owner' | 'editor' | 'viewer'

export type MemberRole = Exclude<TripRole, 'owner'>

export type TripStatus = 'upcoming' | 'ongoing' | 'completed'

export type DocumentKind = 'plan' | 'report'

/** TripUser identifies a person inside a trip. */
export interface TripUser {
  id: string
  display_name: string
  email: string
}

export interface Trip {
  id: string
  /**
   * What the trip holds. A report is a trip of its own, with its own people,
   * links, pictures and budget, rather than a part of a plan.
   */
  kind: DocumentKind
  /** The plan a report was copied from, when the reader may open it. */
  source_trip_id: string | null
  title: string
  summary: string
  /** YYYY-MM-DD; a trip always has a period. */
  start_date: string
  end_date: string
  timezone: string
  currency: string
  travelers: number
  /** Decimal string in the trip currency, such as "1240.50". */
  budget_amount: string | null
  status: TripStatus
  day_count: number
  /** The reader's role. */
  role: TripRole
  owner: TripUser
  /** The plan a plan holds; null for a report. */
  plan_id: string | null
  /** The report a report holds; null for a plan. */
  report_id: string | null
  /** The picture the trip is shown by, out of its own files. */
  cover_media_id: string | null
  /** A report's languages, the original first; empty for a plan. */
  languages: string[]
  /** A report's title and summary in its further languages. */
  translations: TripTranslations
  created_at: string
  updated_at: string
}

/** TripTranslations are a report's own title and summary, by language, then by field. */
export type TripTranslations = Record<string, { title?: string; summary?: string }>

/**
 * DocumentTranslations are a report's words in its further languages: by
 * language, then by the element translated, then by field. A field missing
 * here is read in the original.
 */
export type DocumentTranslations = Record<string, Record<string, Record<string, string>>>

/** TranslationTarget is the kind of element a translation belongs to. */
export type TranslationTarget = 'trip' | 'document' | 'day' | 'stay' | 'item' | 'leg'

/** TranslationEntry is one translated field sent to the server; an empty value removes it. */
export interface TranslationEntry {
  target_type: TranslationTarget
  target_id: string
  field: string
  value: string
}

export interface TripMember {
  user: TripUser
  role: TripRole
  created_at: string
}

/** BackupDestination is where a configuration writes its archives. */
export type BackupDestination = 'local' | 'sftp'

/** BackupStatus is how one attempt ended, or that it is still going. */
export type BackupStatus = 'running' | 'success' | 'failed'

/**
 * BackupConfig is one standing instruction to copy the whole service somewhere.
 *
 * Credentials are listed by name in stored_secrets and never returned: their
 * values are sealed with a key that lives outside the database.
 */
export interface BackupConfig {
  id: string
  destination_type: BackupDestination
  destination_params: Record<string, string>
  stored_secrets: string[]
  encrypted: boolean
  schedule_cron: string
  retain_count: number
  retain_days: number
  enabled: boolean
  next_run_at: string | null
}

/** BackupProgress is how far a run in flight has got. */
export interface BackupProgress {
  stage: 'connecting' | 'reading' | 'archiving' | 'rotating'
  files_done: number
  files_total: number
  bytes_done: number
  bytes_total: number
}

/** BackupRun is one attempt to carry a configuration out. */
export interface BackupRun {
  id: string
  backup_config_id: string
  started_at: string
  finished_at: string | null
  status: BackupStatus
  size_bytes: number | null
  artifact: string
  /** Whether the archive is still at its destination; the name outlives it. */
  archive_held: boolean
  error_message: string
  /** Set while the run works on the server that answered; null otherwise. */
  progress: BackupProgress | null
}

/** Archive is one archive a destination holds, as a restore is offered it. */
export interface Archive {
  name: string
  size_bytes: number
  written_at: string
  /** Read from the name: whether a passphrase will be needed. */
  encrypted: boolean
}

/** RestoreRun is one background whole-instance replacement. */
export interface RestoreRun {
  id: string
  status: 'queued' | 'running' | 'success' | 'failed'
  stage: 'validating' | 'staging' | 'cutover' | 'complete'
  schema_version: number
  tables: number
  trips: number
  files: number
  error_code: string
  error_message: string
}

export interface ErrorBody {
  code: string
  message: string
  details?: Record<string, unknown>
  request_id?: string
}

/** ActivityType says what kind of activity an activity is. */
export type ActivityType =
  | 'hike' | 'walk' | 'bike' | 'run' | 'canyoning' | 'climbing' | 'via_ferrata'
  | 'kayak' | 'swim' | 'ski' | 'tour' | 'other'

export const ACTIVITY_TYPES: readonly ActivityType[] = [
  'hike', 'walk', 'bike', 'run', 'canyoning', 'climbing', 'via_ferrata', 'kayak', 'swim', 'ski', 'tour', 'other',
]

export type PlaceCategory = 'sight' | 'nature' | 'museum' | 'food' | 'shopping' | 'activity' | 'transport' | 'other'

export const PLACE_CATEGORIES: readonly PlaceCategory[] = [
  'sight', 'nature', 'museum', 'food', 'shopping', 'activity', 'transport', 'other',
]

export type CostCategory = 'accommodation' | 'transport' | 'food' | 'activities' | 'shopping' | 'other'

export const COST_CATEGORIES: readonly CostCategory[] = ['accommodation', 'transport', 'food', 'activities', 'shopping', 'other']

export type StayKind = 'hotel' | 'apartment' | 'hostel' | 'camping' | 'friends' | 'other'

export const STAY_KINDS: readonly StayKind[] = ['hotel', 'apartment', 'hostel', 'camping', 'friends', 'other']

/**
 * ItemStatus is how a place of a report turned out. A plan keeps every place at
 * "planned"; a place of a report is "visited" until marked "skipped", and
 * "unplanned" marks one added straight into the report.
 */
export type ItemStatus = 'planned' | 'visited' | 'skipped' | 'unplanned'

/** Schedule counts minutes after the midnight a day starts on; it may pass 1440. */
export interface Schedule {
  arrival_minutes: number
  departure_minutes: number
  late: boolean
}

/** PlanItem is a place, or a stay mark showing its stay's name and position. */
/** Media is one stored file of a trip: a photograph of its report today. */
export interface Media {
  id: string
  trip_id: string
  original_name: string
  mime: string
  size: number
  /** The size in pixels, as the picture is shown. */
  width: number
  height: number
  /** When and where the camera took it; empty when it carries no metadata. */
  taken_at: string | null
  lat: number | null
  lng: number | null
  /** A private file stays out of read-only links that were not given them. */
  is_private: boolean
  /**
   * In the gallery of a day or a place, whether this is one of the pictures the
   * report shows there; in the gallery of the trip, whether it is a favourite
   * anywhere in the report.
   */
  is_favorite: boolean
  status: 'ready' | 'processing' | 'failed'
  created_at: string
}

/**
 * Track is the line a day was really travelled, imported from a GPX or KML
 * file. The file itself is not kept: the line and its length describe it.
 */
export interface Track {
  id: string
  original_name: string
  format: 'gpx' | 'kml'
  /** How far the day went, measured over every point of the file. */
  distance_m: number
  /** How many points that file held, before the line was thinned. */
  point_count: number
  /** Height gained and lost in metres; null when the file records no heights. */
  ascent_m: number | null
  descent_m: number | null
  /** The thinned line, in the encoded polyline format legs use. */
  geometry: string
  /** The first and last moment the file records; null when it records no time. */
  started_at: string | null
  ended_at: string | null
}

/** MediaTarget is what a file is shown under. */
export type MediaTarget = 'trip' | 'day' | 'item'

export interface PlanItem {
  id: string
  day_id: string | null
  position: number
  /** A place or an activity is where the trip goes; a stay mark follows the stays. */
  kind: 'place' | 'activity' | 'stay_anchor'
  anchor: 'morning' | 'evening' | null
  stay_id: string | null
  name: string
  category: PlaceCategory
  /** Set on activities only. */
  activity_type: ActivityType | null
  lat: number | null
  lng: number | null
  address: string
  osm_ref: string
  description_md: string
  url: string
  desired_time: string | null
  visit_minutes: number
  is_optional: boolean
  booking_ref: string
  planned_cost_amount: string | null
  cost_per_person: boolean
  cost_category: CostCategory
  schedule: Schedule | null
  /** The fields below belong to a report; in a plan they are empty. */
  status: ItemStatus
  story_md: string
  actual_time: string | null
  /** When the place was left or the activity finished. */
  actual_end_time: string | null
  rating: number | null
  actual_cost_amount: string | null
  /** How hard an activity is, 1 (very easy) to 5 (extreme); null on a place. */
  difficulty: number | null
  /** The place of the plan this one was copied from. */
  source_item_id: string | null
  /** The place's pictures, in the order they were linked. */
  media: Media[]
  cover_media_id: string | null
  /** The line the place or activity was recorded along. Report only. */
  track: Track | null
}

export type LegSource = 'pending' | 'provider' | 'straight_line' | 'estimate' | 'missing_coordinates'

/** Leg is the journey between two neighbouring elements of a day. */
export interface Leg {
  id: string
  from_item_id: string
  to_item_id: string
  mode: TravelMode
  /** The values the plan uses: typed ones where set. */
  distance_m: number | null
  duration_s: number | null
  calculated_distance_m: number | null
  calculated_duration_s: number | null
  manual_distance: boolean
  manual_duration: boolean
  /** Encoded polyline, precision 5. */
  geometry: string
  source: LegSource
  error: 'provider_disabled' | 'rate_limited' | 'daily_limit' | 'no_route' | 'provider_error' | null
  calculated_at: string | null
  planned_cost_amount: string | null
  /** What was really spent. Report only. */
  actual_cost_amount: string | null
  note: string
}

export interface DaySummary {
  visit_minutes: number
  travel_minutes: number
  distance_m: number
  by_mode: { mode: TravelMode; distance_m: number; duration_s: number }[]
  /** Some leg has no travel time, so the day ends later than shown. */
  unknown_travel: boolean
  pending_legs: number
  /** Legs holding an estimate the provider could be asked about again. */
  estimated_legs: number
  end_minutes: number
  /** What the day plans to spend, optional places included. */
  planned_cost: string
  /** What the day really cost; zero outside a report. */
  actual_cost: string
}

export interface PlanDay {
  id: string
  position: number
  date: string | null
  title: string
  notes_md: string
  start_time: string
  default_mode: TravelMode | null
  timezone: string | null
  morning_anchor: boolean
  evening_anchor: boolean
  no_overnight: boolean
  items: PlanItem[]
  legs: Leg[]
  summary: DaySummary
  /** The day's pictures, in the order they were linked. */
  media: Media[]
  cover_media_id: string | null
}

export interface Stay {
  id: string
  name: string
  kind: StayKind
  address: string
  lat: number | null
  lng: number | null
  check_in_date: string
  check_in_time: string | null
  check_out_date: string
  check_out_time: string | null
  booking_ref: string
  url: string
  contacts: string
  notes_md: string
  planned_cost_amount: string | null
  /** What was really spent. Report only. */
  actual_cost_amount: string | null
  nights: number
  price_per_night: string | null
  source_stay_id: string | null
}

/**
 * Expense is a cost that belongs to no place, stay or leg. day_id is null for an
 * expense of the whole trip; actual_amount and spent_on stay empty in a plan.
 */
export interface Expense {
  id: string
  day_id: string | null
  category: CostCategory
  planned_amount: string | null
  actual_amount: string | null
  spent_on: string | null
  note: string
}

export interface Night {
  date: string
  /** More than one is an overlap. */
  stay_ids: string[]
  no_overnight: boolean
  missing: boolean
}

/** ReportCounts counts the places of a report by status. */
export interface ReportCounts {
  planned: number
  visited: number
  skipped: number
  unplanned: number
  total: number
}

/** ReportTotals is what the reading mode of a report opens with. */
export interface ReportTotals {
  days: number
  /** Nights covered by a stay. */
  nights: number
  places: ReportCounts
  distance_m: number
  by_mode: { mode: TravelMode; distance_m: number; duration_s: number }[]
  /** The snapshot the report carries, against what was really spent. */
  planned_cost: string
  actual_cost: string
  /** Actual minus planned; negative when less was spent. */
  difference: string
  rated: number
  average_rating: number | null
}

export interface TripDocument {
  id: string
  trip_id: string
  kind: DocumentKind
  /** The plan a report was copied from. */
  source_document_id: string | null
  /** The words before and after the days. */
  intro_md: string
  summary_md: string
  days: PlanDay[]
  unassigned: PlanItem[]
  stays: Stay[]
  expenses: Expense[]
  nights: Night[]
  stay_summary: { nights: number; cost: string; average_per_night: string | null }
  /** Legs waiting for a calculation across the document. */
  pending_legs: number
  /** Legs holding an estimate the provider could be asked about again. */
  estimated_legs: number
  /** A report's figures for its reading mode; null on a plan. */
  totals: ReportTotals | null
  /** A report's words in its further languages. */
  translations: DocumentTranslations
  created_at: string
  updated_at: string
}

/** ShareLink is a read-only link as its owner sees it afterwards, without its token. */
export interface ShareLink {
  id: string
  /** The owner's own note, such as "For my parents". */
  label: string
  include_private_media: boolean
  /** When the link stops working; null never expires. */
  expires_at: string | null
  last_used_at: string | null
  use_count: number
  created_at: string
}

/** CreatedShareLink adds the token, shown only once; the page builds the address. */
export interface CreatedShareLink extends ShareLink {
  token: string
}

/** SharedTrip is the trip as a read-only link shows it. */
export interface SharedTrip {
  title: string
  summary: string
  start_date: string
  end_date: string
  timezone: string
  currency: string
  travelers: number
  status: TripStatus
  day_count: number
  owner_name: string
  /** A report's languages, the original first. */
  languages: string[]
  translations: TripTranslations
}

/** Shared is what a share token opens. */
export interface Shared {
  trip: SharedTrip
  label: string
  /** What the trip holds, and so what the link opens. */
  kind: DocumentKind
  include_private_media: boolean
}

/** BudgetEntryKind says what carries a cost. */
export type BudgetEntryKind = 'place' | 'stay' | 'leg' | 'expense'

/** BudgetEntry is one cost of the document, for the expense list and its filters. */
export interface BudgetEntry {
  kind: BudgetEntryKind
  /** The place, stay, leg or expense carrying the cost. */
  id: string
  label: string
  category: CostCategory
  day_id: string | null
  /** What the cost comes to for the whole group. */
  amount: string
  /** The amount as it was typed. */
  unit_amount: string
  per_person: boolean
  /** A place its day may skip; it counts towards `planned` all the same. */
  is_optional: boolean
  /** A place with no day; left out of every total but the budget's `unassigned`. */
  unassigned: boolean
  /**
   * What was really spent for the whole group, in a report; null in a plan and
   * where nothing was entered. A cost entered only as spent has an `amount` of 0.
   */
  actual_amount: string | null
}

export interface BudgetCategoryRow {
  category: CostCategory
  planned: string
  /** What the category really cost; "0.00" in a plan. */
  actual: string
}

export interface BudgetDayRow {
  day_id: string
  position: number
  date: string | null
  planned: string
  /** What the day really cost; "0.00" in a plan. */
  actual: string
}

/**
 * Budget is the money side of a trip's plan or report. Every amount is a decimal
 * string in the trip's currency. `planned` is the sum of the days, `stays` and
 * `untied`, optional places included; only `unassigned` is reported apart, so a
 * place with no day never inflates it.
 *
 * A report's budget carries the planned amounts it was copied with and `actual`
 * beside them, read from the report alone; its overall budget is measured
 * against what was spent.
 */
export interface Budget {
  kind: DocumentKind
  /** The document the figures come from. */
  document_id: string | null
  currency: string
  travelers: number
  budget_amount: string | null
  planned: string
  unassigned: string
  stays: string
  untied: string
  /** Everything a report records as spent; "0.00" in a plan. */
  actual: string
  /** The spending over the travellers: `planned` in a plan, `actual` in a report. */
  per_person: string
  /** The budget minus the spending; negative when overspent, null without a budget. */
  remaining: string | null
  /**
   * The share of the budget the plan takes up, for a bar; null without a budget,
   * and above 100 when the plan does not fit.
   */
  used_percent: number | null
  categories: BudgetCategoryRow[]
  days: BudgetDayRow[]
  entries: BudgetEntry[]
}

/** RemovedDay is a day with content a change would remove. */
export interface RemovedDay {
  document: DocumentKind
  position: number
  date: string | null
  title: string
}
