package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"time"
)

func main() {
	var photo bytes.Buffer
	_ = jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil)
	shot := map[string]any{"jpegBase64": base64.StdEncoding.EncodeToString(photo.Bytes())}
	fmt.Println("whip-computer/1")
	scan := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scan.Scan() {
		var q struct {
			ID     int
			Method string
			Params map[string]any
		}
		if json.Unmarshal(scan.Bytes(), &q) != nil {
			os.Exit(2)
		}
		if q.Params["token"] != os.Getenv("WHIP_COMPUTER_TOKEN") {
			os.Exit(3)
		}
		f, _ := os.OpenFile(os.Args[0]+".calls", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		_, _ = fmt.Fprintln(f, q.Method)
		_ = f.Close()
		var value any
		switch q.Method {
		case "handshake":
			value = map[string]any{"version": "whip-computer/1"}
		case "apps":
			value = []any{map[string]any{"name": "Test App", "bundleId": "com.test.app", "pid": 42, "active": true}}
		case "screenshot":
			value = shot
		case "state", "ax", "click", "type":
			if q.Method == "type" && q.Params["text"] == "block" {
				time.Sleep(time.Hour)
			}
			if q.Method == "click" && q.Params["gen"] != float64(7) {
				os.Exit(4)
			}
			state := map[string]any{"generation": 7, "app": "Test App", "elements": []any{map[string]any{"index": 0, "role": "AXButton", "title": "test"}}}
			if q.Method != "ax" {
				state["screenshot"] = shot
			}
			value = state
		default:
			os.Exit(5)
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": value})
	}
}
