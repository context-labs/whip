# Native Whip components

Import from `../ui`. The library uses React Native text/input/press primitives,
Expo UI sheets, FlashList pickers and the shared theme provider. Product data and
command handling stay in feature/runtime modules. `components/primitives.tsx`
retains older feature names as aliases; it contains no second visual system.

Use Text variants for typography, Surface for raised groups, Button/IconButton
for actions, TextField for labelled editable fields, ListRow for navigation,
ChoiceGroup for exclusive choices, and Sheet/PickerList for focused choices.
Buttons expose loading/disabled state; command owners must retain synchronous
submission guards. Handle action failures next to their control.

Use semantic theme colors, never palette IDs or screen-specific color literals.
System controls own keyboard/back/selection behavior. Text may grow; avoid fixed
row heights. One route owns one sheet; Cancel must discard transient selection.

In development, open `/gallery?theme=claude-code` (or any catalog ID) to inspect
real controls, Markdown, fields and keyboard/sheet behavior without host calls.
Gallery is redirected to home in release builds. Theme definitions and the
portable native adapter remain separate from native components for testing.
