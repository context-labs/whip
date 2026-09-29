package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// fillInput focuses and selects before trusted text insertion, preserving Unicode
// without manufacturing platform-specific hardware key codes.
func fillInput(ctx context.Context, b interface {
	Eval(context.Context, string) (string, error)
	PressKey(context.Context, string) error
	TypeText(context.Context, string) error
}, selector, text string,
) error {
	sel, err := json.Marshal(selector)
	if err != nil {
		return err
	}
	focused, err := b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;e.focus();if(typeof e.select==='function')e.select();else{const s=window.getSelection(),r=document.createRange();r.selectNodeContents(e);s.removeAllRanges();s.addRange(r)}return true})()`, sel))
	if err != nil {
		return err
	}
	if focused != "true" {
		return fmt.Errorf("fill: element not found: %s", selector)
	}
	if text == "" {
		return b.PressKey(ctx, "Backspace")
	}
	return b.TypeText(ctx, text)
}
