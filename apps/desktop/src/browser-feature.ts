// Main decides once at launch; renderer storage and URL parameters are never authority.
export const browserTabsArgument = '--whip-browser-tabs';
export const browserTabsEnabled = (override: string | undefined): boolean => override !== '0';
