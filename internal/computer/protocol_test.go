package computer

import "testing"

func TestScreenshotDecode(t *testing.T) {
	var nilShot *Screenshot
	if b, err := nilShot.Decode(); b != nil || err != nil {
		t.Errorf("nil screenshot: %v %v", b, err)
	}
	if b, err := (&Screenshot{}).Decode(); b != nil || err != nil {
		t.Errorf("empty screenshot: %v %v", b, err)
	}
	b, err := (&Screenshot{JPEGBase64: "aGVsbG8="}).Decode()
	if err != nil || string(b) != "hello" {
		t.Errorf("decode: %q %v", b, err)
	}
	if _, err := (&Screenshot{JPEGBase64: "!!!"}).Decode(); err == nil {
		t.Error("bad base64 must error")
	}
}
