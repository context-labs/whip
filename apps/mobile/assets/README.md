# Mobile assets

`whip-mark.svg` is the editable Whip W mark, using the shared dark canvas and
warm primary color. The opaque 1024px icon is for iOS; Android uses the transparent
foreground and white monochrome variant. The transparent mark also supplies the
native splash image. Keep the outer icon background square; the OS applies its mask.

Font files are individual assets from the pinned Inter and JetBrains Mono npm
packages, imported in `src/theme/fonts.ts`. Their unmodified license notices are
bundled in `font-licenses.json` and available under Settings → Font licenses.
Regenerate those notices from `LICENSE_FONT` when updating either package.
