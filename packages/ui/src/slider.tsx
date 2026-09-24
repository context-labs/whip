import * as stylex from '@stylexjs/stylex';
import {Slider as BaseSlider} from '@base-ui/react/slider';
import {colors, scale, surface} from './tokens.stylex';
import type {Styled} from './actions';

export interface SliderProps extends Styled {
  label: string;
  value: number;
  onValueChange(value: number): void;
  min?: number;
  max?: number;
  step?: number;
  getValueText?(value: number): string;
  disabled?: boolean;
  id?: string;
}
/** A single-value slider. Base UI owns keyboard, touch, and range semantics. */
export function Slider({label, value, onValueChange, min = 0, max = 100, step = 1, getValueText, disabled, id, xstyle}: SliderProps) {
  return <BaseSlider.Root value={value} onValueChange={onValueChange} min={min} max={max} step={step} disabled={disabled} id={id} {...stylex.props(styles.root, disabled && styles.disabled, xstyle)}>
    <BaseSlider.Control {...stylex.props(styles.control)}>
      <BaseSlider.Track data-whip-slider-track {...stylex.props(styles.track)}><BaseSlider.Indicator data-whip-slider-indicator {...stylex.props(styles.indicator)}/></BaseSlider.Track>
      <BaseSlider.Thumb aria-label={label} getAriaValueText={getValueText ? (_, next) => getValueText(next) : undefined} {...stylex.props(styles.thumb)}/>
    </BaseSlider.Control>
  </BaseSlider.Root>;
}
const styles = stylex.create({
  root: {minWidth: 100, width: '100%'},
  disabled: {opacity: 0.45},
  control: {minHeight: 44, width: '100%', display: 'flex', alignItems: 'center', position: 'relative', touchAction: 'none', userSelect: 'none'},
  track: {width: '100%', height: 4, borderRadius: 4, backgroundColor: {default: surface.controlBorder, '@media (forced-colors: active)': 'ButtonText'}, overflow: 'hidden'},
  indicator: {borderRadius: 4, backgroundColor: colors.foreground},
  thumb: {width: 16, height: 16, borderRadius: '50%', backgroundColor: {default: colors.foreground, '@media (forced-colors: active)': 'ButtonText'}, borderWidth: 1, borderStyle: 'solid', borderColor: {default: colors.panel, '@media (forced-colors: active)': 'Canvas'}, outline: {default: 'none', ':focus-within': `2px solid ${colors.borderFocus}`}, outlineOffset: 3, minWidth: {[scale.touch]: 20}, minHeight: {[scale.touch]: 20}},
});
