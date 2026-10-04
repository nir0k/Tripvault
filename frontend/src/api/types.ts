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
  /** Whether ordinary event notifications are emailed; security mail is always sent. */
  email_notifications: boolean
  /** False for a self-registered account whose address is not confirmed yet. */
  email_verified: boolean
  /** When an unconfirmed self-registered account is deleted; null for every other account. */
  delete_after: string | null
  /** Interface language; empty follows the browser. */
  locale: string
  theme: Theme
  /** The instance's colour theme the person reads the interface in; null for the built-in one. */
  theme_id: string | null
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

/** ThemeVariant is the light or the dark palette of a theme. */
export type ThemeVariant = 'light' | 'dark'

/**
 * ThemePalette maps every colour of the interface, named without the
 * "--color-" prefix of its variable, to a colour value.
 */
export type ThemePalette = Record<string, string>

/** InstanceTheme is a colour theme the administrator uploaded for everybody. */
export interface InstanceTheme {
  id: string
  name: string
  /** The light palette; null when the theme has only a dark one. */
  light: ThemePalette | null
  /** The dark palette; null when the theme has only a light one. */
  dark: ThemePalette | null
  created_at: string
  updated_at: string
}

/** ThemeFile is the theme file an administrator uploads and downloads. */
export interface ThemeFile {
  format: 1
  name: string
  light?: ThemePalette
  dark?: ThemePalette
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

/** HomeInput is a home as its owner sets it: a point and the radius hidden around it. */
export interface HomeInput {
  lat: number
  lng: number
  radius_m: number
}

/**
 * HomeSettings is the owner's home and the circle actually hidden, whose centre
 * is moved off the home; both are null when no home is set.
 */
export interface HomeSettings {
  home: HomeInput | null
  zone: HomeInput | null
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
  /** Whether a road leg can be routed by the shortest distance. */
  routing_shortest: boolean
  /** Whether the provider offers other routes than its best to choose from. */
  routing_alternatives: boolean
  /** False when place search and reverse lookups are unavailable. */
  geocoding_enabled: boolean
  /** Raster tile URL template with {z}, {x} and {y}. */
  map_tile_url: string
  /** Attribution the map must show; may contain links. */
  map_attribution: string
  /** Whether SMTP is configured and enabled for recovery and invitations. */
  mail_enabled: boolean
  /** Whether people may register themselves; it needs mail to be delivered. */
  self_registration: boolean
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

/** StorageStatus is the user-file usage and the two configured allowances. */
export interface StorageStatus {
  media_bytes: number
  database_bytes: number
  used_bytes: number
  /** Zero means the operator left the instance unlimited. */
  limit_bytes: number
  /** Zero means an individual trip is unlimited. */
  trip_quota_bytes: number
}

export interface ServiceStatus {
  version: string
  schema_version: number
  users: { total: number; active: number; admins: number }
  routing: ProviderStatus
  geocoding: ProviderStatus
  storage: StorageStatus
  mail: MailStatus
}

/** MailStatus is the operator configuration, administrator switch and delivery queue. */
export interface MailStatus {
  configured: boolean
  enabled: boolean
  /** The administrator's registration switch; registration is open only while mail is configured and enabled too. */
  self_registration: boolean
  queued: number
  failed: number
  last_success_at: string | null
  last_error_at: string | null
  last_error: string
}

/** VerificationSent says when another confirmation message may be requested. */
export interface VerificationSent {
  resend_available_in: number
}

export type InvitationStatus = 'pending' | 'accepted' | 'revoked' | 'expired'

/** UserInvitation is an account invitation without its one-time token. */
export interface UserInvitation {
  id: string
  email: string
  display_name: string
  is_admin: boolean
  status: InvitationStatus
  expires_at: string
  created_at: string
}

/** TripInvitation is an email invitation to one trip. */
export interface TripInvitation {
  id: string
  email: string
  role: MemberRole
  status: InvitationStatus
  expires_at: string
  created_at: string
}

export interface UserInvitationPreview {
  email: string
  display_name: string
  is_admin: boolean
  expires_at: string
}

export interface TripInvitationPreview {
  trip_id: string
  trip_title: string
  email: string
  role: MemberRole
  expires_at: string
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

export type TravelMode =
  | 'walk' | 'car' | 'bike' | 'transit' | 'bus' | 'train' | 'tram' | 'ferry' | 'flight' | 'cable_car' | 'other'

export const TRAVEL_MODES: readonly TravelMode[] = [
  'walk', 'car', 'bike', 'transit', 'bus', 'train', 'tram', 'ferry', 'flight', 'cable_car', 'other',
]

export type TripRole = 'owner' | 'editor' | 'viewer'

export type MemberRole = Exclude<TripRole, 'owner'>

/**
 * TripStatus is where a plan stands: upcoming, ongoing and overdue follow its
 * dates while it is open, completed and cancelled are set by hand. A report has
 * none.
 */
export type TripStatus = 'upcoming' | 'ongoing' | 'overdue' | 'completed' | 'cancelled'

/** PlanState is how a plan is closed by hand; null opens it again. */
export type PlanState = 'completed' | 'cancelled'

export type DocumentKind = 'plan' | 'report'

/** TagColor is a colour of the palette tags and packing categories are drawn in. */
export type TagColor =
  | 'red' | 'orange' | 'amber' | 'green' | 'teal' | 'sky' | 'blue' | 'violet' | 'pink' | 'gray'
  | 'yellow' | 'emerald' | 'indigo' | 'fuchsia' | 'lime' | 'cyan' | 'purple' | 'rose' | 'brown'

/**
 * TAG_COLORS is the palette in the order the server gives it to what is made
 * without a colour, neighbours far apart in hue.
 */
export const TAG_COLORS: readonly TagColor[] = [
  'red', 'orange', 'amber', 'green', 'teal', 'sky', 'blue', 'violet', 'pink', 'gray',
  'yellow', 'emerald', 'indigo', 'fuchsia', 'lime', 'cyan', 'purple', 'rose', 'brown',
]

/** TAG_COLOR_CHOICES is the palette as it is chosen from: round the colour wheel, the neutrals last. */
export const TAG_COLOR_CHOICES: readonly TagColor[] = [
  'red', 'rose', 'pink', 'fuchsia', 'purple', 'violet', 'indigo', 'blue', 'sky', 'cyan',
  'teal', 'emerald', 'green', 'lime', 'yellow', 'amber', 'orange', 'brown', 'gray',
]

/** TripTag is one of the reader's own tags as a trip wears it. */
export interface TripTag {
  id: string
  name: string
  color: TagColor
}

/** Tag is one of the reader's own tags, as the list of them shows it. */
export interface Tag extends TripTag {
  /** How many of the trips the reader can open wear it. */
  trip_count: number
  /** How many of the reader's ideas wear it. */
  idea_count: number
  created_at: string
}

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
  /**
   * Whether a report was copied from a plan, even one since deleted; a place
   * added to it arrives marked as not planned.
   */
  from_plan: boolean
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
  /** Where a plan stands; absent for a report. */
  status?: TripStatus
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
  /** The part of the cover the trip is shown by; null means its middle. */
  cover_crop: CoverCrop | null
  /** A report's languages, the original first; empty for a plan. */
  languages: string[]
  /** A report's title and summary in its further languages. */
  translations: TripTranslations
  created_at: string
  updated_at: string
  /** The reader's own tags on the trip, by name; nobody else sees them. */
  tags: TripTag[]
  /** The speed on the flat, in km/h, a plan's lines are timed at unless a line names its own. */
  track_speed_kmh: number
}

/**
 * CoverCrop is a frame inside a picture, as fractions of its width and height
 * from the top left corner of the picture as it is shown upright.
 */
export interface CoverCrop {
  x: number
  y: number
  w: number
  h: number
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
export type TranslationTarget = 'trip' | 'document' | 'day' | 'stay' | 'transfer' | 'item' | 'leg' | 'stop'

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

/** BackupCheckStep is a step of a destination check, named when it fails. */
export type BackupCheckStep = 'credentials' | 'connect' | 'sign_in' | 'directory' | 'write' | 'delete' | 'list'

/**
 * BackupCheck is what trying a destination found. A failure names the step and
 * carries the server's own reason; host_key is the key an SFTP server presented
 * to a form that pins none.
 */
export interface BackupCheck {
  ok: boolean
  step?: BackupCheckStep
  message?: string
  host_key?: string
  archives: number
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

export type PlaceCategory =
  | 'sight' | 'nature' | 'museum' | 'food' | 'shopping' | 'activity' | 'transport' | 'parking' | 'other'

export const PLACE_CATEGORIES: readonly PlaceCategory[] = [
  'sight', 'nature', 'museum', 'food', 'shopping', 'activity', 'transport', 'parking', 'other',
]

/**
 * CostCategory is a budget category. The keys are stored and never renamed:
 * accommodation reads as lodging and food as restaurants and bars.
 */
export type CostCategory =
  | 'accommodation' | 'transport' | 'car_rental' | 'fuel' | 'tolls' | 'food' | 'groceries'
  | 'sightseeing' | 'activities' | 'shopping' | 'other'

export const COST_CATEGORIES: readonly CostCategory[] = [
  'accommodation', 'transport', 'car_rental', 'fuel', 'tolls', 'food', 'groceries',
  'sightseeing', 'activities', 'shopping', 'other',
]

export type StayKind = 'hotel' | 'apartment' | 'hostel' | 'camping' | 'friends' | 'other'

export const STAY_KINDS: readonly StayKind[] = ['hotel', 'apartment', 'hostel', 'camping', 'friends', 'other']

/** TransferKind says how a booked journey travels; `transfer` is a car booked to take the travellers somewhere. */
export type TransferKind = 'flight' | 'train' | 'bus' | 'ferry' | 'transfer' | 'other'

export const TRANSFER_KINDS: readonly TransferKind[] = ['flight', 'train', 'bus', 'ferry', 'transfer', 'other']

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
  status: 'ready'
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
  /**
   * How many metres of the line run at each slope, in whole percent from -50
   * (index 0) to +50 (index 100); null when the file records no heights.
   */
  grades: number[] | null
  /** The speed on the flat the line is timed at, in km/h; null takes the plan's. */
  speed_kmh: number | null
  /** The first and last moment the file records; null when it records no time. */
  started_at: string | null
  ended_at: string | null
  /** The stops along the line, the nearest its start first. */
  stops: Stop[]
}

/** StopKind says what a stop along a line is for. */
export type StopKind =
  | 'food' | 'shop' | 'rest' | 'viewpoint' | 'water' | 'shelter' | 'hut' | 'summit' | 'cave' | 'swim' | 'other'

export const STOP_KINDS: readonly StopKind[] = [
  'food', 'shop', 'rest', 'viewpoint', 'water', 'shelter', 'hut', 'summit', 'cave', 'swim', 'other',
]

/**
 * Stop is somewhere along the line of an activity: a café halfway up a hike, a
 * rest by a lake. It costs like a place, in the activity's day.
 */
export interface Stop {
  id: string
  kind: StopKind
  /** The stop's own name; empty when its kind says enough. */
  name: string
  note_md: string
  /** Where it lies, on the line. */
  lat: number
  lng: number
  /** How far along the line from its start it lies. */
  distance_m: number
  /** The metres of the line up to it at each slope, as Track.grades; null without heights. */
  grades_to: number[] | null
  /** When it was reached. Report only. */
  actual_time: string | null
  planned_cost_amount: string | null
  actual_cost_amount: string | null
  cost_per_person: boolean
  cost_category: CostCategory
  cost_note: string
  paid_by: string | null
  cost_split: CostSplit
  cost_shares: CostShare[]
}

/**
 * Attachment is a file a place or an activity carries besides its pictures, such
 * as a ticket or a booking. A read-only link never sees one.
 */
export interface Attachment {
  id: string
  original_name: string
  /** A line on what the file is; empty when nobody wrote one. */
  description: string
  /** What the server recognised the file as. */
  mime: string
  size: number
  created_at: string
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
  /** What the cost is for, in a few words. */
  cost_note: string
  /** The member who pays the cost; null when nobody was named. */
  paid_by: string | null
  cost_split: CostSplit
  /** The members the cost is shared among, in the order they were listed. */
  cost_shares: CostShare[]
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
  /** The files the place carries, oldest first; always empty through a read-only link. */
  attachments: Attachment[]
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
  /** What a road route is optimised for. */
  route_preference: RoutePreference
  /** The points a road route passes through, in order. */
  via: GeoPoint[]
  /** A route chosen among the alternatives, kept until the leg changes. */
  route_pinned: boolean
  /**
   * The parts of a journey with changes, in order; empty for a leg travelled
   * one way. The leg's own figures, line, cost and source are then worked out
   * from them.
   */
  segments: LegSegment[]
  /** What the parts of a journey with changes were paid with. */
  tickets: LegTicket[]
}

/** LegSegment is one part of a journey with changes. */
export interface LegSegment {
  id: string
  mode: TravelMode
  distance_m: number | null
  duration_s: number | null
  calculated_distance_m: number | null
  calculated_duration_s: number | null
  manual_distance: boolean
  manual_duration: boolean
  /** Encoded polyline, precision 5. */
  geometry: string
  source: LegSource
  error: Leg['error']
  /** The ticket the part travels on, or null. */
  ticket_id: string | null
  /** The change where the part ends; null for the last part. */
  stop: LegStop | null
}

/** LegStop is a change on the way: a station, a stop, a pier. */
export interface LegStop {
  name: string
  lat: number | null
  lng: number | null
  /** The time spent there before the next part leaves. */
  wait_minutes: number
}

/** LegTicket is what one or more parts of a journey with changes were paid with. */
export interface LegTicket {
  id: string
  name: string
  planned_cost_amount: string | null
  /** What it really cost. Report only. */
  actual_cost_amount: string | null
}

/** RoutePreference is what a road route is optimised for. */
export type RoutePreference = 'fastest' | 'shortest'

/** GeoPoint is a position on Earth. */
export interface GeoPoint {
  lat: number
  lng: number
}

/** RouteOption is one road route a leg could take. */
export interface RouteOption {
  distance_m: number
  duration_s: number
  /** Encoded polyline, precision 5. */
  geometry: string
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
  /** The moment a report's day is remembered by, in a line; empty in a plan. */
  highlight: string
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
 * Transfer is a booked journey between two places - a flight, a train, a ferry.
 * It belongs to the whole document and shows in the day it departs on and the
 * day it arrives on, without taking part in the day's schedule.
 */
export interface Transfer {
  id: string
  kind: TransferKind
  /** The carrier or the number, such as "Icelandair FI 204"; may be empty. */
  name: string
  from_name: string
  from_address: string
  from_lat: number | null
  from_lng: number | null
  to_name: string
  to_address: string
  to_lat: number | null
  to_lng: number | null
  departure_date: string
  departure_time: string | null
  /** Null when the transfer arrives on the day it departs. */
  arrival_date: string | null
  arrival_time: string | null
  booking_ref: string
  url: string
  notes_md: string
  planned_cost_amount: string | null
  /** The amounts are per traveller and multiplied in the budget. */
  cost_per_person: boolean
  /** What was really spent. Report only. */
  actual_cost_amount: string | null
  source_transfer_id: string | null
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
  /** What the money went on, in a few words. */
  note: string
  /** Anything longer said about the expense, as plain text. */
  comment: string
  /** A link to a booking, a receipt or a shop; empty when there is none. */
  url: string
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
  /** By departure date and time. */
  transfers: Transfer[]
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
  /** Whether the link may save the pictures, one by one or as an archive. */
  allow_download: boolean
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
  /** Where a plan stands; absent for a report. */
  status?: TripStatus
  day_count: number
  owner_name: string
  /** A report's languages, the original first. */
  languages: string[]
  translations: TripTranslations
  /** The speed a plan's lines are timed at unless a line names its own. */
  track_speed_kmh: number
}

/** Shared is what a share token opens. */
export interface Shared {
  trip: SharedTrip
  label: string
  /** What the trip holds, and so what the link opens. */
  kind: DocumentKind
  include_private_media: boolean
  /** Whether the link may save the pictures. */
  allow_download: boolean
}

/** BudgetEntryKind says what carries a cost. */
export type BudgetEntryKind = 'place' | 'stop' | 'stay' | 'transfer' | 'leg' | 'expense'

/** BudgetEntry is one cost of the document, for the expense list and its filters. */
export interface BudgetEntry {
  kind: BudgetEntryKind
  /** The place, stay, transfer, leg or expense carrying the cost. */
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
 * string in the trip's currency. `planned` is the sum of the days, `stays`,
 * `transfers` and `untied`, optional places included; only `unassigned` is reported apart, so a
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
  transfers: string
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
  /** Where each member stands across the split costs; empty while nothing is split. */
  balances: MemberBalance[]
  /** The payments that square the balances, largest first. */
  settlements: Settlement[]
}

/**
 * CostSplit is how a cost is shared among the members of a trip: not at all,
 * equally among the members listed, or in amounts of each member's own.
 */
export type CostSplit = 'none' | 'everyone' | 'individuals'

/** CostShare is one member's part of a split cost; amount is null for an equal split. */
export interface CostShare {
  user_id: string
  amount: string | null
}

/** MemberBalance is where one member stands across the split costs of a trip. */
export interface MemberBalance {
  user_id: string
  /** Empty for an account that is gone. */
  name: string
  paid: string
  share: string
  /** What the others owe the member; negative when the member owes them. */
  net: string
}

/** Settlement is one payment that squares the members' accounts. */
export interface Settlement {
  from_user_id: string
  from_name: string
  to_user_id: string
  to_name: string
  amount: string
}

/** RemovedDay is a day with content a change would remove. */
export interface RemovedDay {
  document: DocumentKind
  position: number
  date: string | null
  title: string
}

/** PackingIcon is a key of the fixed set of icons a packing category is shown with. */
export type PackingIcon =
  | 'documents' | 'tickets' | 'cards' | 'money' | 'coins' | 'keys' | 'work' | 'clothes' | 'bags' | 'luggage'
  | 'glasses' | 'rain' | 'first_aid' | 'medicine' | 'thermometer' | 'hygiene' | 'health' | 'cosmetics'
  | 'electronics' | 'phone' | 'chargers' | 'power_bank' | 'headphones' | 'camera' | 'flash_drive' | 'watch'
  | 'games' | 'food' | 'snacks' | 'drinks' | 'groceries' | 'books' | 'music' | 'drawing' | 'toys' | 'gifts'
  | 'kids' | 'sport' | 'swimming' | 'beach' | 'snow' | 'camping' | 'lamp' | 'compass' | 'map' | 'binoculars'
  | 'hiking' | 'sleep' | 'nature' | 'gear' | 'flight' | 'train' | 'bus' | 'car' | 'fuel' | 'home' | 'tools'
  | 'stationery' | 'shopping' | 'other'

/**
 * PACKING_ICON_KEYS is the set in the order it is chosen from: things that go
 * together stand together, documents first and the catch-all last.
 */
export const PACKING_ICON_KEYS: readonly PackingIcon[] = [
  'documents', 'tickets', 'cards', 'money', 'coins', 'keys', 'work', 'clothes', 'bags', 'luggage', 'glasses',
  'rain', 'first_aid', 'medicine', 'thermometer', 'hygiene', 'health', 'cosmetics', 'electronics', 'phone',
  'chargers', 'power_bank', 'headphones', 'camera', 'flash_drive', 'watch', 'games', 'food', 'snacks',
  'drinks', 'groceries', 'books', 'music', 'drawing', 'toys', 'gifts', 'kids', 'sport', 'swimming', 'beach',
  'snow', 'camping', 'lamp', 'compass', 'map', 'binoculars', 'hiking', 'sleep', 'nature', 'gear', 'flight',
  'train', 'bus', 'car', 'fuel', 'home', 'tools', 'stationery', 'shopping', 'other',
]

/**
 * PackingCategory is one heading of a plan's packing list, named by the trip.
 * Its colour, a key of the tags' palette, and its icon only tell it apart.
 */
export interface PackingCategory {
  id: string
  name: string
  color: TagColor
  icon: PackingIcon
  position: number
}

/** PackingItem is one thing to take; one tick serves the whole group. */
export interface PackingItem {
  id: string
  /** Null for an item without a category, listed after the categories. */
  category_id: string | null
  name: string
  quantity: number
  note: string
  packed: boolean
  /** The member who brings it; always null through a read-only link. */
  bringer_id: string | null
  position: number
}

/** PackingList is a plan's whole list, with how much of it is packed. */
export interface PackingList {
  categories: PackingCategory[]
  items: PackingItem[]
  packed: number
  total: number
}

/** VisaRequirement says whether a visa is needed to go; on_arrival is one got online or at the border. */
export type VisaRequirement = 'unknown' | 'not_needed' | 'needed' | 'on_arrival'

/** VISA_REQUIREMENTS lists them in the order they are offered. */
export const VISA_REQUIREMENTS: readonly VisaRequirement[] = ['not_needed', 'on_arrival', 'needed', 'unknown']

/** IdeaCost is one part of an idea's rough cost besides getting there, which each way prices itself. */
export type IdeaCost = 'stay' | 'food' | 'other'

/** IDEA_COSTS lists the parts of an idea's cost in the order they are shown. */
export const IDEA_COSTS: readonly IdeaCost[] = ['stay', 'food', 'other']

/** IdeaCosts are the rough costs of an idea's whole trip as decimal strings; null while unknown. */
export type IdeaCosts = Record<IdeaCost, string | null>

/** IdeaPhoto is one picture of an idea, as big as it is kept. */
export interface IdeaPhoto {
  id: string
  width: number
  height: number
}

/** IdeaPlace is one place an idea goes to: a name, and a point when one was found. */
export interface IdeaPlace {
  name: string
  lat: number | null
  lng: number | null
}

/**
 * IdeaTransport is one way of getting there, an alternative to the others:
 * one way of travelling, or several mixed along one route.
 */
export interface IdeaTransport {
  modes: TravelMode[]
  /** A decimal string; null while unknown. */
  cost: string | null
  minutes: number | null
}

/**
 * Idea is somewhere the reader would like to go one day. It is the reader's
 * own, like a tag, and not a trip: a plan is made from it when the time comes.
 */
export interface Idea {
  id: string
  title: string
  /** ISO 3166-1 alpha-2 codes, the main country first. */
  countries: string[]
  places: IdeaPlace[]
  /** The idea's pictures in their order, read from /ideas/{id}/photos/{photoId}. */
  photos: IdeaPhoto[]
  /** The best months for going, 1 to 12; none is not said. */
  months: number[]
  days_min: number | null
  days_max: number | null
  /** The best length, between the two. */
  days_ideal: number | null
  description_md: string
  currency: string
  costs: IdeaCosts
  transports: IdeaTransport[]
  /** The cheapest and the dearest way of getting there; null while none is priced. */
  transport_min: string | null
  transport_max: string | null
  /** The whole trip with the cheapest and with the dearest way; null while nothing is priced. */
  cost_min: string | null
  cost_max: string | null
  visa: VisaRequirement
  tags: TripTag[]
  created_at: string
  updated_at: string
}

/** IdeaFields are the fields an idea is saved with, all of them at once. */
export type IdeaFields = Omit<
  Idea, 'id' | 'photos' | 'transport_min' | 'transport_max' | 'cost_min' | 'cost_max' | 'tags' | 'created_at'
  | 'updated_at'
>
