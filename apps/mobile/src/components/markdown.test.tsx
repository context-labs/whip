import { render } from '@testing-library/react-native';
import { Linking } from 'react-native';
import { Markdown } from './markdown';

const mockNativeMarkdown = jest.fn();
jest.mock('react-native-enriched-markdown', () => ({ EnrichedMarkdownText: (props: unknown) => { mockNativeMarkdown(props); return null; } }));
jest.mock('../theme/theme', () => ({ useTheme: () => ({ colors: { foreground: '#fff', muted: '#aaa', link: '#aaf', element: '#222', border: '#444' }, code: { foreground: '#fff', background: '#222' }, markdown: { code: '#aaf', quote: '#aaa' } }) }));
jest.mock('@expo/ui', () => ({ Host: require('react-native').View, Column: require('react-native').View, Button: () => null }));

beforeEach(() => { mockNativeMarkdown.mockClear(); });

test('embedded Markdown images and raw HTML remain selectable source without invoking native Markdown', async () => {
  for (const text of ['![private image](https://example.test/image.png)', '<img src="https://example.test/image.png">']) {
    const screen = await render(<Markdown text={text} />);
    expect(screen.getByText(text).props.selectable).toBe(true);
    expect(mockNativeMarkdown).not.toHaveBeenCalled();
    await screen.unmount();
  }
});

test('native Markdown disables previews and unsafe link callbacks cannot open a URL', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);
  try {
    await render(<Markdown text="[safe link](https://example.test)" />);
    const props = mockNativeMarkdown.mock.calls.at(-1)![0];
    expect(props.enableLinkPreview).toBe(false);
    for (const url of ['javascript:alert(1)', 'file:///private/file', 'whip://server', 'https://user:secret@example.test']) props.onLinkPress({ url });
    expect(open).not.toHaveBeenCalled();
    props.onLinkPress({ url: 'https://example.test' });
    expect(open).toHaveBeenCalledWith('https://example.test/');
  } finally { open.mockRestore(); }
});
