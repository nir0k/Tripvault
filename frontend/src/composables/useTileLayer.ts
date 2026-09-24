import { ref, type Ref } from 'vue'
import L from 'leaflet'
import DOMPurify from 'dompurify'

// The tile server is the operator's choice, and the thing most likely to be
// wrong about a fresh deployment: a network that refuses it, or an address of
// one's own on plain http, which the page is not allowed to load images from. A
// map of empty squares says none of that, so the layer watches its own failures
// and lets the map explain itself.
//
// A server that answers with a picture saying the access was blocked is not one
// of those cases: as far as the browser is concerned the tile arrived. That one
// is only ever fixed by naming another server.

/**
 * tileFailureThreshold is how many refused tiles mean the server rather than the
 * edge of its coverage: a map asks for a dozen at a time, and one missing at the
 * end of the world is not a fault.
 */
const tileFailureThreshold = 3

/** TileLayer is a map's tiles, and whether the server behind them answers. */
export interface TileLayer {
  /** failed is true once the server has refused enough tiles to say so. */
  failed: Ref<boolean>
  /** host names the server the tiles are asked of, for the message. */
  host: Ref<string>
  /** attach adds the tiles to a map, once it exists. */
  attach: (map: L.Map, tileUrl: string, attribution: string) => void
}

/** useTileLayer prepares a map's tiles and watches whether they arrive. */
export function useTileLayer(): TileLayer {
  const failed = ref(false)
  const host = ref('')
  let refused = 0
  let loaded = 0

  function attach(map: L.Map, tileUrl: string, attribution: string): void {
    host.value = hostOf(tileUrl)

    const layer = L.tileLayer(tileUrl, {
      attribution: DOMPurify.sanitize(attribution, { ALLOWED_TAGS: ['a'], ALLOWED_ATTR: ['href'] }),
      maxZoom: 19,
    })
    layer.on('tileerror', () => {
      refused += 1
      // Tiles that do arrive mean the server is there, so a few refusals are
      // gaps in its coverage rather than a map nobody can read.
      failed.value = loaded === 0 && refused >= tileFailureThreshold
    })
    layer.on('tileload', () => {
      loaded += 1
      failed.value = false
    })
    layer.addTo(map)
  }

  return { failed, host, attach }
}

/** hostOf names the server the tiles were asked of. */
function hostOf(tileUrl: string): string {
  try {
    return new URL(tileUrl).host
  } catch {
    return tileUrl
  }
}
