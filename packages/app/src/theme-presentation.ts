import type { Resolved } from '@whip/protocol';
export function themeFromHost(value: Resolved, namespace: string) {
  const { on_primary, border_focus, diff_add, diff_del, ...colors } =
    value.colors;
  return {
    ...value,
    id: `${namespace}:${value.id}`,
    web: value.web ? {
      ...(value.web.navigation ? {navigation: value.web.navigation} : {}),
      ...(value.web.quiet_border ? {quietBorder: value.web.quiet_border} : {}),
      ...(value.web.code_background ? {codeBackground: value.web.code_background} : {}),
      ...(value.web.inline_code_background ? {inlineCodeBackground: value.web.inline_code_background} : {}),
    } : undefined,
    colors: {
      ...colors,
      onPrimary: on_primary,
      borderFocus: border_focus,
      diffAdd: diff_add,
      diffDel: diff_del,
    },
  };
}
