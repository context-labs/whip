import * as stylex from '@stylexjs/stylex';
import { Input as BaseInput } from '@base-ui/react/input';
import { Field as BaseField } from '@base-ui/react/field';
import { Fieldset as BaseFieldset } from '@base-ui/react/fieldset';
import { Checkbox as BaseCheckbox } from '@base-ui/react/checkbox';
import { Switch as BaseSwitch } from '@base-ui/react/switch';
import { Radio as BaseRadio } from '@base-ui/react/radio';
import { RadioGroup as BaseRadioGroup } from '@base-ui/react/radio-group';
import { Select as BaseSelect } from '@base-ui/react/select';
import { Combobox as BaseCombobox } from '@base-ui/react/combobox';
import { NumberField as BaseNumberField } from '@base-ui/react/number-field';
import { mergeProps } from '@base-ui/react/merge-props';
import { Check, ChevronDown, Minus, Plus } from 'lucide-react';
import { useId, useMemo } from 'react';
import type { ComponentPropsWithRef, ReactNode } from 'react';
import { styles } from './styles.stylex';
import type { Styled } from './actions';

export function Input({xstyle, className, ...props}: ComponentPropsWithRef<'input'> & Styled) {
  return <BaseInput {...mergeProps(stylex.props(styles.control, styles.input, xstyle), props)} className={state => mergeProps(stylex.props(styles.control, styles.input, (state.valid === false || props['aria-invalid'] === true || props['aria-invalid'] === 'true') && styles.invalid, xstyle), {className}).className}/>;
}
export function Textarea({xstyle, className, ...props}: ComponentPropsWithRef<'textarea'> & Styled) {
  return <BaseField.Control render={<textarea/>} {...mergeProps(stylex.props(styles.control, styles.input, styles.textarea, xstyle), props)} className={state => mergeProps(stylex.props(styles.control, styles.input, styles.textarea, (state.valid === false || props['aria-invalid'] === true || props['aria-invalid'] === 'true') && styles.invalid, xstyle), {className}).className}/>;
}
export function Field({label, description, error, children, htmlFor, xstyle}: {label: ReactNode; description?: ReactNode; error?: ReactNode; children: ReactNode; htmlFor?: string} & Styled) {
  return <BaseField.Root invalid={Boolean(error)} {...stylex.props(styles.field, xstyle)}><BaseField.Label htmlFor={htmlFor} {...stylex.props(styles.label)}>{label}</BaseField.Label>{children}{description && <BaseField.Description {...stylex.props(styles.description)}>{description}</BaseField.Description>}{error && <BaseField.Error match {...stylex.props(styles.error)}>{error}</BaseField.Error>}</BaseField.Root>;
}
export function Fieldset({legend, description, children, xstyle}: {legend: ReactNode; description?: ReactNode; children: ReactNode} & Styled) {
  return <BaseFieldset.Root {...stylex.props(styles.stack, styles.fieldset, xstyle)}><BaseFieldset.Legend {...stylex.props(styles.label)}>{legend}</BaseFieldset.Legend>{description && <p {...stylex.props(styles.description)}>{description}</p>}{children}</BaseFieldset.Root>;
}
export function Label({xstyle, ...props}: ComponentPropsWithRef<'label'> & Styled) {return <label {...mergeProps(stylex.props(styles.label, xstyle), props)}/>;}

type CheckProps = BaseCheckbox.Root.Props & Styled & {label?: ReactNode; description?: string};
export function Checkbox({label, description, xstyle, className, style, ...props}: CheckProps) {
  const id = useId();
  return <label {...stylex.props(styles.inline, styles.label, styles.checkLabel)}><BaseCheckbox.Root {...props} aria-describedby={[props['aria-describedby'], description ? id : undefined].filter(Boolean).join(' ') || undefined} style={state => mergeProps(stylex.props(xstyle), {style: typeof style === 'function' ? style(state) : style}).style} className={state => mergeProps(stylex.props(styles.control, styles.checkbox, state.checked && styles.checked, xstyle), {className: typeof className === 'function' ? className(state) : className}).className}><BaseCheckbox.Indicator><Check size={13}/></BaseCheckbox.Indicator></BaseCheckbox.Root>{(label || description) && <span>{label}{description && <span id={id} {...stylex.props(styles.description, styles.optionDescription)}>{description}</span>}</span>}</label>;
}
export function Switch({label, description, xstyle, className, style, ...props}: BaseSwitch.Root.Props & Styled & {label?: ReactNode; description?: string}) {
  const id = useId();
  return <label {...stylex.props(styles.inline, styles.label, styles.checkLabel)}><BaseSwitch.Root {...props} aria-describedby={[props['aria-describedby'], description ? id : undefined].filter(Boolean).join(' ') || undefined} style={state => mergeProps(stylex.props(xstyle), {style: typeof style === 'function' ? style(state) : style}).style} className={state => mergeProps(stylex.props(styles.control, styles.switch, state.checked && styles.switchOn, xstyle), {className: typeof className === 'function' ? className(state) : className}).className}><BaseSwitch.Thumb className={state => stylex.props(styles.switchThumb, state.checked && styles.switchThumbOn).className}/></BaseSwitch.Root>{(label || description) && <span>{label}{description && <span id={id} {...stylex.props(styles.description, styles.optionDescription)}>{description}</span>}</span>}</label>;
}
export function RadioGroup({value, onValueChange, options, label, disabled}: {value?: string; onValueChange?: (value: string) => void; options: Option[]; label: string; disabled?: boolean}) {
  const id = useId();
  return <BaseRadioGroup value={value} onValueChange={value => onValueChange?.(value as string)} aria-label={label} aria-labelledby={undefined} disabled={disabled} {...stylex.props(styles.stack)}>{options.map((option, index) => <label key={option.value} {...stylex.props(styles.inline, styles.label, styles.checkLabel)}><BaseRadio.Root value={option.value} disabled={option.disabled} aria-describedby={option.description ? `${id}-${index}` : undefined} className={state => stylex.props(styles.control, styles.checkbox, styles.radio, state.checked && styles.checked).className}><BaseRadio.Indicator><Check size={12}/></BaseRadio.Indicator></BaseRadio.Root><span>{option.label}{option.description && <span id={`${id}-${index}`} {...stylex.props(styles.description, styles.optionDescription)}>{option.description}</span>}</span></label>)}</BaseRadioGroup>;
}
export interface Option {value: string; label: string; description?: string; disabled?: boolean; icon?: ReactNode}
export interface SelectProps extends Styled {
  value?: string | null; defaultValue?: string; onValueChange?: (value: string) => void; options: readonly Option[];
  label: string; placeholder?: string; disabled?: boolean; id?: string; name?: string; required?: boolean;
}
export function Select({value, defaultValue, onValueChange, options, label, placeholder = 'Select…', disabled, id, name, required, xstyle}: SelectProps) {
  return <BaseSelect.Root value={value} defaultValue={defaultValue} onValueChange={next => {if (next !== null) onValueChange?.(next);}} items={options} disabled={disabled} name={name} required={required} id={id}><BaseSelect.Trigger aria-label={label} {...stylex.props(styles.control, styles.button, xstyle)}><BaseSelect.Value placeholder={placeholder}/><BaseSelect.Icon {...stylex.props(styles.chevron)}><ChevronDown size={14}/></BaseSelect.Icon></BaseSelect.Trigger><BaseSelect.Portal><BaseSelect.Positioner sideOffset={6} align="start" alignItemWithTrigger={false} {...stylex.props(styles.positioner)}><BaseSelect.Popup {...stylex.props(styles.popup)}><BaseSelect.List>{options.map(option => <BaseSelect.Item key={option.value} value={option.value} disabled={option.disabled} className={state => stylex.props(styles.item, state.highlighted && styles.highlighted, state.disabled && styles.disabled).className}><span {...stylex.props(styles.checkSlot)}><BaseSelect.ItemIndicator><Check size={13}/></BaseSelect.ItemIndicator></span>{option.icon}<BaseSelect.ItemText>{option.label}</BaseSelect.ItemText></BaseSelect.Item>)}</BaseSelect.List></BaseSelect.Popup></BaseSelect.Positioner></BaseSelect.Portal></BaseSelect.Root>;
}
export function Combobox({value, defaultValue, onValueChange, options, label, placeholder = 'Search…', disabled, id, name, required, xstyle, loading, emptyMessage = 'No matching results', onInputValueChange, onHighlightedValueChange, ref}: SelectProps & {ref?: ComponentPropsWithRef<'input'>['ref']; loading?: boolean; emptyMessage?: string; onInputValueChange?: (text: string) => void; onHighlightedValueChange?: (value: string | undefined) => void}) {
  const collection = useMemo(() => BaseCombobox.createItems(options, {getValue: item => item.value, getLabel: item => item.label}), [options]);
  return <BaseCombobox.Root items={collection} value={value} defaultValue={defaultValue} onValueChange={next => {if (next !== null) onValueChange?.(next);}} onInputValueChange={onInputValueChange} onItemHighlighted={onHighlightedValueChange} disabled={disabled} id={id} name={name} required={required} autoHighlight><BaseCombobox.Input ref={ref} aria-label={label} placeholder={placeholder} {...stylex.props(styles.control, styles.input, xstyle)}/><BaseCombobox.Portal><BaseCombobox.Positioner sideOffset={6} align="start" {...stylex.props(styles.positioner)}><BaseCombobox.Popup {...stylex.props(styles.popup)}>{loading && <p role="status" {...stylex.props(styles.item, styles.description)}>Loading…</p>}<BaseCombobox.Empty {...stylex.props(styles.item, styles.description)}>{loading ? '' : emptyMessage}</BaseCombobox.Empty><BaseCombobox.List>{(option: Option) => <BaseCombobox.Item key={option.value} value={option.value} disabled={option.disabled} className={state => stylex.props(styles.item, state.highlighted && styles.highlighted, state.disabled && styles.disabled).className}><span {...stylex.props(styles.checkSlot)}><BaseCombobox.ItemIndicator><Check size={13}/></BaseCombobox.ItemIndicator></span>{option.icon}<span>{option.label}{option.description && <span {...stylex.props(styles.description, styles.optionDescription)}>{option.description}</span>}</span></BaseCombobox.Item>}</BaseCombobox.List></BaseCombobox.Popup></BaseCombobox.Positioner></BaseCombobox.Portal></BaseCombobox.Root>;
}
export function NumberField({label, value, onValueChange, min, max, step = 1, disabled, xstyle}: {label: string; value?: number | null; onValueChange?: (value: number | null) => void; min?: number; max?: number; step?: number; disabled?: boolean} & Styled) {
  return <BaseNumberField.Root value={value} onValueChange={onValueChange} min={min} max={max} step={step} disabled={disabled} {...stylex.props(styles.field, xstyle)}><BaseNumberField.Group {...stylex.props(styles.inline)}><BaseNumberField.Decrement aria-label={`Decrease ${label}`} {...stylex.props(styles.control, styles.button, styles.icon)}><Minus size={14}/></BaseNumberField.Decrement><BaseNumberField.Input aria-label={label} {...stylex.props(styles.control, styles.input)}/><BaseNumberField.Increment aria-label={`Increase ${label}`} {...stylex.props(styles.control, styles.button, styles.icon)}><Plus size={14}/></BaseNumberField.Increment></BaseNumberField.Group></BaseNumberField.Root>;
}
