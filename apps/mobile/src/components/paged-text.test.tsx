import { fireEvent, render } from '@testing-library/react-native';
import { PagedText, TEXT_PAGE_SIZE, TEXT_PREVIEW_SIZE, textPageStarts, textPreview } from './paged-text';

jest.mock('../theme/theme', () => jest.requireActual('../theme/theme'));
jest.mock('@expo/ui', () => ({ Host: require('react-native').View, Column: require('react-native').View, Button: () => null }));
jest.mock('./markdown', () => ({ Markdown: ({ text }: { text: string }) => require('react').createElement(require('react-native').Text, { testID: 'native-markdown' }, text) }));

test('pages preserve every code point, never split surrogate pairs and never exceed the native bound', () => {
  const text = `${'a'.repeat(TEXT_PAGE_SIZE - 1)}😀${'b'.repeat(TEXT_PAGE_SIZE - 1)}𐍈${'c'.repeat(TEXT_PAGE_SIZE * 2)}`;
  const starts = textPageStarts(text);
  const pages = starts.map((start, index) => text.slice(start, starts[index + 1]));
  expect(pages.join('')).toBe(text);
  for (const page of pages) {
    expect(page.length).toBeLessThanOrEqual(TEXT_PAGE_SIZE);
    expect(page).not.toMatch(/^[\uDC00-\uDFFF]|[\uD800-\uDBFF]$/);
  }
  expect(textPageStarts('')).toEqual([0]);
  expect(textPageStarts('a'.repeat(TEXT_PAGE_SIZE))).toEqual([0]);
  const preview = textPreview(`${'a'.repeat(TEXT_PREVIEW_SIZE - 2)}😀tail`);
  expect(preview.length).toBeLessThanOrEqual(TEXT_PREVIEW_SIZE);
  expect(preview).not.toMatch(/[\uD800-\uDBFF]…$/);
});

test('short text keeps Markdown; large text uses selectable pages and bounded navigation', async () => {
  const screen = await render(<PagedText identity="row" text="**Short Markdown**" />);
  expect(screen.getByTestId('native-markdown')).toHaveTextContent('**Short Markdown**');
  expect(screen.queryByLabelText('Next text page')).toBeNull();
  const text = `${'A'.repeat(TEXT_PAGE_SIZE)}${'B'.repeat(TEXT_PAGE_SIZE)}end`;
  await screen.rerender(<PagedText identity="row" text={text} />);
  expect(screen.queryByTestId('native-markdown')).toBeNull();
  expect(screen.getByTestId('source-text-page').props.selectable).toBe(true);
  expect(screen.getByTestId('source-text-page').props.children).toBe('A'.repeat(TEXT_PAGE_SIZE));
  expect(screen.getByLabelText('Previous text page')).toBeDisabled();
  await fireEvent.press(screen.getByLabelText('Next text page'));
  expect(screen.getByTestId('source-text-page').props.children).toBe('B'.repeat(TEXT_PAGE_SIZE));
  await fireEvent.press(screen.getByLabelText('Next text page'));
  expect(screen.getByTestId('source-text-page').props.children).toBe('end');
  expect(screen.getByLabelText('Next text page')).toBeDisabled();
});

test('live appends retain the selected page; recycling resets it and shrinkage clamps it', async () => {
  const text = `${'A'.repeat(TEXT_PAGE_SIZE)}${'B'.repeat(TEXT_PAGE_SIZE)}tail`;
  const screen = await render(<PagedText identity="first" text={text} />);
  await fireEvent.press(screen.getByLabelText('Next text page'));
  await screen.rerender(<PagedText identity="first" text={`${text}${'C'.repeat(TEXT_PAGE_SIZE * 2)}`} />);
  expect(screen.getByTestId('source-text-page').props.children).toBe('B'.repeat(TEXT_PAGE_SIZE));
  await screen.rerender(<PagedText identity="next" text={text} />);
  expect(screen.getByTestId('source-text-page').props.children).toBe('A'.repeat(TEXT_PAGE_SIZE));
  await fireEvent.press(screen.getByLabelText('Next text page'));
  await fireEvent.press(screen.getByLabelText('Next text page'));
  await screen.rerender(<PagedText identity="next" text="short" />);
  await screen.rerender(<PagedText identity="next" text={text} />);
  expect(screen.getByTestId('source-text-page').props.children).toBe('A'.repeat(TEXT_PAGE_SIZE));
});
