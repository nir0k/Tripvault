import { onBeforeUnmount, onMounted, useTemplateRef } from 'vue'

/**
 * useDropdown drives a DaisyUI dropdown built on <details>. Unlike the
 * focus-based variant it stays open while a control inside it, such as a
 * native <select>, takes the focus. It closes on a click outside and on Escape;
 * the caller closes it after a choice through close().
 *
 * key is the template ref name given to the <details> element. The variable
 * holding the result must not carry that same name: the template compiler binds
 * a setup binding of the ref's name as the element's ref, and this object is not
 * a ref, so the render crashes on the next update.
 */
export function useDropdown(key: string) {
  const root = useTemplateRef<HTMLDetailsElement>(key)

  /** close folds the dropdown if it is open. */
  function close(): void {
    if (root.value) {
      root.value.open = false
    }
  }

  // onPointerDown closes the dropdown when the press lands outside it.
  function onPointerDown(event: PointerEvent): void {
    if (root.value?.open && !root.value.contains(event.target as Node)) {
      close()
    }
  }

  // onKeyDown closes the dropdown on Escape and returns focus to its toggle.
  function onKeyDown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && root.value?.open) {
      close()
      root.value.querySelector('summary')?.focus()
    }
  }

  onMounted(() => {
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('pointerdown', onPointerDown)
    document.removeEventListener('keydown', onKeyDown)
  })

  return { close }
}

/**
 * useDropdownGroup drives the <details> dropdowns repeated down a list - the
 * menu a gallery carries on every tile. A <details> knows nothing about its
 * neighbours, so on its own a press outside leaves the menu open and every tile
 * touched adds another open menu to the page. This folds every open menu a
 * press did not land in, and all of them on Escape. The menu the press did land
 * in is left alone, so a second press on the same summary closes it the way the
 * browser does instead of being reopened by the toggle.
 *
 * The caller folds a menu after a choice through close(), which takes the event
 * of the click - a list has no single element to hold a template ref on.
 */
export function useDropdownGroup() {
  /** close folds the menu the event was handled in. */
  function close(event: Event): void {
    const menu = (event.currentTarget as HTMLElement | null)?.closest('details.dropdown')
    if (menu instanceof HTMLDetailsElement) {
      menu.open = false
    }
  }

  // closeAll folds every open menu on the page except the one holding the node.
  function closeAll(except: Node | null): void {
    for (const menu of document.querySelectorAll<HTMLDetailsElement>('details.dropdown[open]')) {
      if (!except || !menu.contains(except)) {
        menu.open = false
      }
    }
  }

  // onPointerDown folds the menus the press landed outside of.
  function onPointerDown(event: PointerEvent): void {
    closeAll(event.target as Node)
  }

  // onKeyDown folds every open menu on Escape, wherever the focus happens to be.
  function onKeyDown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      closeAll(null)
    }
  }

  onMounted(() => {
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('pointerdown', onPointerDown)
    document.removeEventListener('keydown', onKeyDown)
  })

  return { close }
}
