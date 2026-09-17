// Main decides once at launch; renderer storage and URL parameters are never authority.
export const browserTabsArgument = '--whip-browser-tabs';
export const browserTabsEnabled = (packaged: boolean, override: string | undefined): boolean => !packaged || override === '1';
