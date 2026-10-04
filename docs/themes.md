# Colour themes

A colour theme changes the colours of the Tripvault interface for everybody who picks it. An administrator uploads themes under Administration → Themes, and every person picks one in their profile. This document describes the theme file: what each colour is for, which values are accepted, and how to make a theme that reads well.

## Making a theme

1. Open Administration → Themes and click **Download template**. The template holds the built-in light and dark palettes, so every colour starts at the value the interface uses today.
2. Change the colours you want and give the theme a `name` of its own.
3. Click **Upload theme**. The page shows each palette as a strip of colours and a small piece of interface, so you can judge it before anybody picks it.
4. To change a theme later, edit its file and use **Replace with a file** from the theme's menu. People who picked the theme stay on it and see the new colours the next time they load a page. **Delete** returns them to the built-in theme.

**Download file** in the same menu gives back the file exactly as it is stored, which is the easiest way to start a second theme from the first.

## The file

```json
{
  "format": 1,
  "name": "Forest",
  "light": {
    "base-100": "#ffffff",
    "base-200": "#f0fdf4",
    "base-300": "#dcfce7",
    "base-content": "#1c1917",
    "primary": "#166534",
    "primary-content": "#ffffff",
    "...": "..."
  },
  "dark": {
    "base-100": "#0b1f14",
    "...": "..."
  }
}
```

| Field | Meaning |
|---|---|
| `format` | The version of the file format. Always `1`. |
| `name` | The name people see in their profile, 1 to 50 characters. Two themes cannot share a name, whatever the letter case. |
| `light` | The palette for the light mode. Optional. |
| `dark` | The palette for the dark mode. Optional. |

A theme needs at least one of `light` and `dark`:

- **With both**, the person's light / dark / auto setting picks the palette, as it does with the built-in theme.
- **With one**, the interface is always shown in that palette. The light / dark switch is disabled and says why. The person's setting is kept and applies again when they pick another theme.

The file is JSON, so it cannot hold comments, and a palette cannot hold keys of its own: the server refuses any name that is not one of the colours below.

## Colour values

Every colour is written as one of:

- a hexadecimal colour: `#rgb`, `#rgba`, `#rrggbb` or `#rrggbbaa`, for example `#166534`;
- a CSS colour function: `rgb()`, `rgba()`, `hsl()`, `hsla()`, `hwb()`, `lab()`, `lch()`, `oklab()` or `oklch()`, for example `oklch(70% 0.1 150)` or `rgb(22 101 52 / 90%)`.

Colour names (`red`), variables (`var(--x)`) and anything else are refused. The server writes the values into the page's style unchanged, so it accepts only what can be nothing but a colour. Values are stored in lower case.

When a file is refused, the page names the colour and the problem, for example `dark.accent - not a hexadecimal colour or a CSS colour function`.

## The colours

Every palette has to set all 20 colours. They come in pairs: a colour and its `-content` colour, which is the colour of text and icons drawn on top of it. Change both together and check that the text still reads.

### Page and text

| Colour | What it is for in Tripvault |
|---|---|
| `base-100` | The main background: the page, cards, dialogs, menus, form fields. Also the fill of some map markers and the background that tag and packing colours are mixed into. |
| `base-200` | The second background, a step away from `base-100`: the top bar, the bottom bar on a phone, the background of the sign-in, registration and shared-link pages, and the editor's toolbar. |
| `base-300` | Above all the colour of borders and dividers: the edge of every card, list and table, the line under the top bar, and the lines between rows. It is a fill only in a few small places, such as the track of a slider. Keep it clearly darker than `base-100` in a light palette (lighter in a dark one) - see [Readability](#readability). |
| `base-content` | Text and icons on any of the three backgrounds. Secondary text, such as dates and hints, is this colour at reduced opacity, so it has to keep enough contrast against `base-100` when faded to about 60 %. Tag and packing colours are mixed with it for their text. |

`base-100`, `base-200` and `base-300` should be a scale: lighter to darker in a light palette, and usually darker to lighter in a dark one. `base-200` may sit very close to `base-100`, because the top bar is set apart from the page by a `base-300` line rather than by its own colour.

A common mistake is to take the subtle background colour of another design system for `base-300`. Most systems keep a separate border colour, and that is the one `base-300` needs: in GitHub's palette, for example, `base-300` is the border colour `#d0d7de`, not the background `#eaeef2`. With the background colour the edges of cards vanish and the top bar merges with the page.

### Brand and highlights

| Colour | What it is for in Tripvault |
|---|---|
| `primary` | The main action colour: the main button on each page and in each dialog, links, the selected menu item, toggles, radio buttons and checkboxes, the focus outline of fields, the icon on a trip card without a cover, and the line of the chosen route on a map. It is the colour people notice most. |
| `primary-content` | Text and icons on `primary`, for example the label of a main button. |
| `secondary` | A second highlight. It blends with `primary` in the placeholder of a trip without a cover and marks one of the stays in the strip of nights of a plan. |
| `secondary-content` | Text on `secondary`. |
| `accent` | A third highlight, used for another stay in the strip of nights. |
| `accent-content` | Text on `accent`. |
| `neutral` | A dark, quiet colour: the badge of a completed plan, the badge of a place category that has no colour of its own, and the alternative routes drawn beside the chosen one on a map. |
| `neutral-content` | Text on `neutral`. |

### Status colours

These carry meaning, so keep them recognisable: a person should still read an error as an error.

| Colour | What it is for in Tripvault |
|---|---|
| `info` | Information notices on a plan, a report and the status page, informational badges, and one of the stays in the strip of nights. |
| `info-content` | Text on `info`. |
| `success` | Confirmations ("Saved", "Theme uploaded"), success notices and badges, and a stay in the strip of nights. |
| `success-content` | Text on `success`. |
| `warning` | Warnings and the badges that need attention, such as a temporary password, and a stay in the strip of nights. |
| `warning-content` | Text on `warning`. |
| `error` | Error messages under forms, destructive actions such as **Delete**, the error notices and badges, and the border of the account deletion card. |
| `error-content` | Text on `error`. |

`error`, `success` and `info` are used both as fills and as **text on the page background**, for example an error message under a form. In a light palette they have to be dark enough to read on `base-100`, and in a dark palette light enough.

## Readability

The server does not check contrast, so a theme can be uploaded even when it is hard to read. Before uploading, check at least these pairs against the WCAG AA level of 4.5:1 for normal text:

- `base-content` on `base-100` and on `base-200`;
- every `X-content` on its `X`, such as `primary-content` on `primary`;
- `error`, `success` and `info` on `base-100`.

Borders are not text and need less contrast, but they still have to be seen. Keep `base-300` at about 1.4:1 or more against `base-100`; the built-in light palette has 1.44:1. Below about 1.25:1 the edges of cards and the line under the top bar disappear on most screens.

Any online contrast checker will do. The preview on the themes page is the quickest overall check: the buttons, badges, field and coloured text in it are drawn exactly as the interface draws them.

## What a theme does not change

- **Maps.** The map tiles, the colours of days on the map and the route lines keep their own colours, so a route reads the same on every theme.
- **Tags and packing categories.** Their colours are a fixed palette chosen to read in both modes. A theme only changes the background and text they are mixed with.
- **PDF documents.** Reports, plans and packing lists are printed in their own colours, so they print well on any printer.
- **Sizes, corner radii and fonts.** A theme sets colours only.
