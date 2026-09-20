# Composer scroll jump — 2026-09-17

The composer's autosize effect set its textarea height to zero, read scrollHeight,
and restored the calculated height on every draft change. That forced layout
temporarily enlarged the sibling transcript viewport. The browser clamped the
transcript's scrollTop to the temporarily lower maximum. When the composer
returned to its original height, ResizeObserver saw no net size change, leaving
the scroll position displaced until another layout correction.

Reproduced against the 10,000-message fixture: a three-line, 79px input returned
to 79px after typing, but transcript scrollTop changed from 859 to 820 (39px).
The fix reserves the composer box's current height during measurement and
releases that reservation after applying the final input height. Actual growth
and shrinkage still work; transcript scrolling and SDK state are unchanged.

The shared browser regression checks every animation frame while typing slowly
enough to include draft persistence and during faster typing. It covers an
attachment, multiline input, reading older content, maximum-height input, narrow
panes, and actual draft growth/shrinkage. All four typing cases showed zero pixels
of movement in Chromium, Firefox, and staged Electron.

Validation: 42 composer/reading unit tests, shared app typecheck, production web
and desktop builds, Electron's full chat activity fixture, and the large-history
performance fixture passed. The performance fixture retained its row/data bounds
and paging anchors under 16 concurrent synthetic streams. Keyboard-to-next-paint
p95 was bounded to 60–68ms in that deliberately loaded run, within the range of
previous recorded runs (52–60ms and 68–76ms); this is not a controlled benchmark
comparison. Measurements are retained in `evidence/composer-scroll/`.

Tested renderer: `8fc0a70e48f400002deb93ea34961770d0ef772074256946a9da0ad0c5c5ebe7`.
The installed user app was not replaced; Electron validation used an isolated
staged build.
