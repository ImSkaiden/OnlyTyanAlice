package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StationPollResponse struct {
	OK          bool             `json:"ok"`
	Commands    []StationCommand `json:"commands"`
	GlagolToken string           `json:"glagol_token"`
}

type StationCommand struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

type GlagolSlot struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type GlagolFormUpdate struct {
	Name  string       `json:"name"`
	Slots []GlagolSlot `json:"slots"`
}

type GlagolServerActionPayload struct {
	FormUpdate GlagolFormUpdate `json:"form_update"`
	Resubmit   bool             `json:"resubmit"`
}

type GlagolServerActionEventPayload struct {
	Type    string                    `json:"type"`
	Name    string                    `json:"name"`
	Payload GlagolServerActionPayload `json:"payload"`
}

type GlagolPayload struct {
	Command                  string                          `json:"command"`
	ServerActionEventPayload *GlagolServerActionEventPayload `json:"serverActionEventPayload,omitempty"`
	Volume                   float64                         `json:"volume,omitempty"`
}

type GlagolMessage struct {
	ConversationToken string        `json:"conversationToken"`
	ID                string        `json:"id"`
	Payload           GlagolPayload `json:"payload"`
}

var (
	tokenCacheMu sync.Mutex
	cachedToken  string
	tokenExpiry  time.Time
)

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func sendWSFrame(conn io.Writer, opcode byte, payload []byte) error {
	var frame []byte
	frame = append(frame, 0x80|opcode)

	l := len(payload)
	if l <= 125 {
		frame = append(frame, 0x80|byte(l))
	} else if l <= 65535 {
		frame = append(frame, 0x80|126, byte(l>>8), byte(l&0xFF))
	} else {
		return fmt.Errorf("payload too large")
	}

	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	frame = append(frame, mask...)

	masked := make([]byte, l)
	for i := 0; i < l; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame = append(frame, masked...)

	_, err := conn.Write(frame)
	return err
}

var (
	ledCancelMu sync.Mutex
	ledCancelFn context.CancelFunc
)

func writeRingLeds(r, g, b int) {
	if r < 0 {
		r = 0
	} else if r > 255 {
		r = 255
	}
	if g < 0 {
		g = 0
	} else if g > 255 {
		g = 255
	}
	if b < 0 {
		b = 0
	} else if b > 255 {
		b = 255
	}

	rVal := []byte(strconv.Itoa(r))
	gVal := []byte(strconv.Itoa(g))
	bVal := []byte(strconv.Itoa(b))

	// Allwinner / Mini 2 ring sysfs (/sys/class/leds/*:ring*/brightness)
	for i := 1; i <= 6; i++ {
		for _, prefix := range []string{"mini2", "yandex"} {
			rp := fmt.Sprintf("/sys/class/leds/%s:red:ring%d/brightness", prefix, i)
			gp := fmt.Sprintf("/sys/class/leds/%s:green:ring%d/brightness", prefix, i)
			bp := fmt.Sprintf("/sys/class/leds/%s:blue:ring%d/brightness", prefix, i)
			if _, err := os.Stat(rp); err == nil {
				_ = os.WriteFile(rp, rVal, 0644)
			}
			if _, err := os.Stat(gp); err == nil {
				_ = os.WriteFile(gp, gVal, 0644)
			}
			if _, err := os.Stat(bp); err == nil {
				_ = os.WriteFile(bp, bVal, 0644)
			}
		}
	}
	// Fallback glob
	if redMatches, _ := filepath.Glob("/sys/class/leds/*:red:ring*/brightness"); len(redMatches) > 0 {
		for _, m := range redMatches {
			_ = os.WriteFile(m, rVal, 0644)
		}
	}
	if greenMatches, _ := filepath.Glob("/sys/class/leds/*:green:ring*/brightness"); len(greenMatches) > 0 {
		for _, m := range greenMatches {
			_ = os.WriteFile(m, gVal, 0644)
		}
	}
	if blueMatches, _ := filepath.Glob("/sys/class/leds/*:blue:ring*/brightness"); len(blueMatches) > 0 {
		for _, m := range blueMatches {
			_ = os.WriteFile(m, bVal, 0644)
		}
	}
}

func hsvToRgb(h, s, v float64) (int, int, int) {
	var r, g, b float64
	i := math.Floor(h * 6)
	f := h*6 - i
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return int(r * 255), int(g * 255), int(b * 255)
}

func setStationLed(color, effect string) {
	r, g, b := 0, 0, 0
	switch strings.ToLower(color) {
	case "purple", "фиолетовый", "малиновый", "magenta":
		r, g, b = 200, 0, 255
	case "pink", "розовый":
		r, g, b = 255, 40, 140
	case "blue", "синий":
		r, g, b = 0, 80, 255
	case "cyan", "бирюзовый", "голубой":
		r, g, b = 0, 255, 200
	case "green", "зеленый", "зелёный":
		r, g, b = 0, 255, 0
	case "yellow", "желтый", "жёлтый":
		r, g, b = 255, 200, 0
	case "red", "красный":
		r, g, b = 255, 0, 0
	case "white", "белый":
		r, g, b = 255, 255, 255
	case "off", "выключить", "black":
		r, g, b = 0, 0, 0
	default:
		r, g, b = 180, 0, 255
	}

	ledCancelMu.Lock()
	if ledCancelFn != nil {
		ledCancelFn()
		ledCancelFn = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	ledCancelFn = cancel
	ledCancelMu.Unlock()

	go func() {
		defer cancel()
		// Mini 1 hardware led_util fallback if sysfs leds are absent
		if matches, _ := filepath.Glob("/sys/class/leds/*ring*/brightness"); len(matches) == 0 {
			if _, err := os.Stat("/system/vendor/quasar/led_util"); err == nil {
				_ = exec.Command("killall", "-9", "led_util").Run()
				if (r != 0 || g != 0 || b != 0) && effect != "off" && color != "off" && color != "black" {
					pattern := "/system/vendor/quasar/ledpatterns/alice_listening.led"
					if effect == "pulse" || effect == "breathe" {
						pattern = "/system/vendor/quasar/ledpatterns/alice_thinking.led"
					} else if effect == "rainbow" {
						pattern = "/system/vendor/quasar/ledpatterns/theremin_easter_5.led"
					} else if color == "red" {
						pattern = "/system/vendor/quasar/ledpatterns/mute_mics.led"
					} else if color == "blue" {
						pattern = "/system/vendor/quasar/ledpatterns/bluetooth_playing.led"
					} else if color == "green" || color == "yellow" {
						pattern = "/system/vendor/quasar/ledpatterns/notification_available.led"
					}
					cmd := exec.Command("/system/vendor/quasar/led_util", pattern)
					cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH=/system/vendor/quasar/libs")
					_ = cmd.Start()
					go func() {
						<-ctx.Done()
						if cmd.Process != nil {
							_ = cmd.Process.Kill()
						}
					}()
				}
				return
			}
		}

		if (r == 0 && g == 0 && b == 0) || effect == "off" {
			writeRingLeds(0, 0, 0)
			return
		}

		if effect == "breathe" || effect == "pulse" {
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			step := 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					step = (step + 1) % 100
					factor := 0.55 + 0.45*math.Sin(float64(step)*2.0*math.Pi/100.0)
					writeRingLeds(int(float64(r)*factor), int(float64(g)*factor), int(float64(b)*factor))
				}
			}
		} else if effect == "rainbow" {
			ticker := time.NewTicker(60 * time.Millisecond)
			defer ticker.Stop()
			hue := 0.0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					hue += 0.02
					if hue > 1.0 {
						hue -= 1.0
					}
					rr, gg, bb := hsvToRgb(hue, 1.0, 1.0)
					writeRingLeds(rr, gg, bb)
				}
			}
		} else {
			writeRingLeds(r, g, b)
		}
	}()
	log.Printf("[LED] Set color: %s (R:%d G:%d B:%d), effect: %s", color, r, g, b, effect)
}

func getRealVolume() int {
	cmd := exec.Command("/bin/sh", "-c", "amixer sget 'Master' 2>/dev/null || /usr/bin/amixer sget 'Master' 2>/dev/null || LD_LIBRARY_PATH=/system/vendor/quasar/libs /system/workdir/bin/amixer sget 'Master' 2>/dev/null")
	out, err := cmd.Output()
	if err != nil {
		return 50
	}

	re := regexp.MustCompile(`\[([0-9]+)%\]`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) >= 2 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			return val
		}
	}
	return 50
}

func setRealVolume(percent int, token string) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// 1. Set ALSA mixer (via vendor script or direct amixer fallback)
	if err := exec.Command("/system/vendor/quasar/set_volume.sh", strconv.Itoa(percent)).Run(); err != nil {
		_ = exec.Command("/bin/sh", "-c", fmt.Sprintf("amixer set 'Master' %d%% 2>/dev/null || /usr/bin/amixer set 'Master' %d%% 2>/dev/null", percent, percent)).Run()
	}

	// 2. Set Spotify Connect volume if installed
	_ = exec.Command("/data/opt/spotify-connect/cli.sh", "volume", strconv.Itoa(percent)).Run()

	// 3. Update Glagol if token is present
	if token != "" {
		volFloat := float64(percent) / 100.0
		_ = sendGlagolSimple(token, GlagolPayload{
			Command: "setVolume",
			Volume:  volFloat,
		})
	}
	log.Printf("[Volume] Set volume to %d%%", percent)
}

func sendGlagolSimple(token string, payload GlagolPayload) error {
	conf := &tls.Config{InsecureSkipVerify: true}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", "127.0.0.1:1961", conf)
	if err != nil {
		return err
	}
	defer conn.Close()

	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	secKey := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: 127.0.0.1:1961\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", secKey)
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil || resp.StatusCode != 101 {
		return fmt.Errorf("handshake failed")
	}

	msg := GlagolMessage{
		ConversationToken: token,
		ID:                newUUID(),
		Payload:           payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return sendWSFrame(conn, 0x1, data)
}

func speakViaGlagol(token, text string) error {
	return sendGlagolSimple(token, GlagolPayload{
		Command: "serverAction",
		ServerActionEventPayload: &GlagolServerActionEventPayload{
			Type: "server_action",
			Name: "update_form",
			Payload: GlagolServerActionPayload{
				FormUpdate: GlagolFormUpdate{
					Name: "personal_assistant.scenarios.quasar.iot.repeat_phrase",
					Slots: []GlagolSlot{
						{
							Type:  "string",
							Name:  "phrase_to_repeat",
							Value: text,
						},
					},
				},
				Resubmit: true,
			},
		},
	})
}

func detectDeviceID(explicitID string) string {
	if explicitID != "" {
		return explicitID
	}
	// 1. Saved in /data/opt/etc/device_id
	if data, err := os.ReadFile("/data/opt/etc/device_id"); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}
	// 2. /system/vendor/quasar/q device-id
	if out, err := exec.Command("/system/vendor/quasar/q", "device-id").Output(); err == nil {
		re := regexp.MustCompile(`Device ID:\s*([A-Za-z0-9]+)`)
		if m := re.FindStringSubmatch(string(out)); len(m) >= 2 {
			id := strings.TrimSpace(m[1])
			_ = os.MkdirAll("/data/opt/etc", 0755)
			_ = os.WriteFile("/data/opt/etc/device_id", []byte(id), 0644)
			return id
		}
	}
	// 3. Fallback to /proc/device-tree/serial-number
	if data, err := os.ReadFile("/proc/device-tree/serial-number"); err == nil {
		id := strings.Trim(strings.TrimSpace(string(data)), "\x00")
		if id != "" {
			_ = os.MkdirAll("/data/opt/etc", 0755)
			_ = os.WriteFile("/data/opt/etc/device_id", []byte(id), 0644)
			return id
		}
	}
	return "FF98F029E01F3BF90B1FC3EA"
}

func getLocalGlagolToken(deviceID string) (string, error) {
	tokenCacheMu.Lock()
	if cachedToken != "" && time.Now().Before(tokenExpiry) {
		tok := cachedToken
		tokenCacheMu.Unlock()
		return tok, nil
	}
	tokenCacheMu.Unlock()

	// 1. Читаем OAuth токен владельца напрямую из хранилища прошивки
	data, err := os.ReadFile("/data/quasar/account_storage.dat")
	if err != nil {
		data, err = os.ReadFile("/bkupgrade/data/quasar/account_storage.dat")
	}
	if err != nil {
		return "", fmt.Errorf("account_storage.dat not found: %w", err)
	}

	re := regexp.MustCompile(`y0_[A-Za-z0-9_-]+`)
	oauthMatch := re.Find(data)
	if oauthMatch == nil {
		return "", fmt.Errorf("oauth token not found in account_storage.dat")
	}
	oauthToken := string(oauthMatch)

	// 2. Определение платформы устройства (Hardware Matrix)
	platform := "yandexmini"
	if dt, err := os.ReadFile("/proc/device-tree/compatible"); err == nil {
		s := string(dt)
		if strings.Contains(s, "mini2") || strings.Contains(s, "sun50iw9") {
			platform = "yandexmini_2"
		} else if strings.Contains(s, "micro") || strings.Contains(s, "rk3308") {
			platform = "yandexmicro"
		} else if strings.Contains(s, "plum") {
			platform = "yandexplum"
		} else if strings.Contains(s, "midi") || strings.Contains(s, "cucumber") {
			platform = "yandexstation_midi"
		} else if strings.Contains(s, "yandexstation_2") || strings.Contains(s, "s905x2") {
			platform = "yandexstation_2"
		} else if strings.Contains(s, "yandexmax") || strings.Contains(s, "s905x3") {
			platform = "yandexmax"
		} else if strings.Contains(s, "yandexstation") || strings.Contains(s, "s905x") {
			platform = "yandexstation"
		}
	}

	targetURL := fmt.Sprintf("https://quasar.yandex.net/glagol/token?device_id=%s&platform=%s", deviceID, platform)
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "OAuth "+oauthToken)

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("glagol cloud request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("glagol cloud status %d", resp.StatusCode)
	}

	var res struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Token == "" {
		return "", fmt.Errorf("empty token in response")
	}

	tokenCacheMu.Lock()
	cachedToken = res.Token
	tokenExpiry = time.Now().Add(50 * time.Minute)
	tokenCacheMu.Unlock()

	log.Printf("[Glagol] Successfully refreshed native conversation token for %s [%s]", deviceID, platform)
	return res.Token, nil
}

func speak(deviceID, text, serverToken string) {
	if text == "" {
		return
	}
	token := serverToken
	if token == "" {
		var err error
		token, err = getLocalGlagolToken(deviceID)
		if err != nil {
			log.Printf("[Glagol Token Error] %v", err)
			return
		}
	}

	if err := speakViaGlagol(token, text); err != nil {
		log.Printf("[Glagol Error] %v, retrying with fresh token...", err)
		tokenCacheMu.Lock()
		cachedToken = ""
		tokenCacheMu.Unlock()
		freshToken, err2 := getLocalGlagolToken(deviceID)
		if err2 == nil {
			if err3 := speakViaGlagol(freshToken, text); err3 != nil {
				log.Printf("[Glagol Retry Error] %v", err3)
			} else {
				log.Printf("[Glagol] Spoke after token refresh: %s", text)
			}
		} else {
			log.Printf("[Glagol Refresh Error] %v", err2)
		}
	} else {
		log.Printf("[Glagol] Spoke: %s", text)
	}
}

func main() {
	serverURL := flag.String("server", "http://195.62.49.84:8088", "OnlyTyan server base URL")
	explicitDev := flag.String("device", "", "Device ID (override)")
	sayText := flag.String("say", "", "Direct phrase to speak and exit")
	flag.Parse()

	deviceID := detectDeviceID(*explicitDev)

	if *sayText != "" {
		log.Printf("One-shot say command requested for %s: %s", deviceID, *sayText)
		speak(deviceID, *sayText, "")
		return
	}

	log.Printf("Starting OnlyTyan Station Daemon [Device: %s, Server: %s]...", deviceID, *serverURL)
	httpClient := &http.Client{Timeout: 8 * time.Second}

	for {
		curVol := getRealVolume()
		pollURL := fmt.Sprintf("%s/api/station/poll?device_id=%s&volume=%d", strings.TrimRight(*serverURL, "/"), url.QueryEscape(deviceID), curVol)

		req, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		req.Header.Set("X-Station-Device-Id", deviceID)

		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		var pollResp StationPollResponse
		err = json.NewDecoder(resp.Body).Decode(&pollResp)
		_ = resp.Body.Close()

		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if pollResp.OK && len(pollResp.Commands) > 0 {
			token := pollResp.GlagolToken
			for _, cmd := range pollResp.Commands {
				log.Printf("[Command] Type=%s ID=%s", cmd.Type, cmd.ID)
				switch cmd.Type {
				case "say":
					text, _ := cmd.Payload["text"].(string)
					if text != "" {
						speak(deviceID, text, token)
					}

				case "volume":
					var pct int
					if val, ok := cmd.Payload["level"].(float64); ok {
						if val <= 10 {
							pct = int(val * 10)
						} else {
							pct = int(val)
						}
					} else if val, ok := cmd.Payload["percent"].(float64); ok {
						pct = int(val)
					} else {
						pct = 50
					}
					setRealVolume(pct, token)

				case "pause":
					_ = exec.Command("/data/opt/spotify-connect/cli.sh", "pause").Run()
					if token != "" {
						_ = sendGlagolSimple(token, GlagolPayload{Command: "stop"})
					}
					log.Printf("[Playback] Paused")

				case "resume", "play":
					_ = exec.Command("/data/opt/spotify-connect/cli.sh", "resume").Run()
					if token != "" {
						_ = sendGlagolSimple(token, GlagolPayload{Command: "play"})
					}
					log.Printf("[Playback] Resumed")

				case "next":
					_ = exec.Command("/data/opt/spotify-connect/cli.sh", "next").Run()
					if token != "" {
						_ = sendGlagolSimple(token, GlagolPayload{Command: "next"})
					}
					log.Printf("[Playback] Next track")

				case "prev", "previous":
					_ = exec.Command("/data/opt/spotify-connect/cli.sh", "previous").Run()
					if token != "" {
						_ = sendGlagolSimple(token, GlagolPayload{Command: "prev"})
					}
					log.Printf("[Playback] Previous track")

				case "eyes":
					raw, err := json.Marshal(cmd.Payload)
					if err == nil {
						c := &http.Client{Timeout: 2 * time.Second}
						resp, err := c.Post("http://127.0.0.1:1213/api/state", "application/json", bytes.NewReader(raw))
						if err != nil {
							log.Printf("[Eyes Error] %v", err)
						} else {
							_ = resp.Body.Close()
							log.Printf("[Eyes] Updated eyes panel state: %s", string(raw))
						}
					}

				case "led":
					color, _ := cmd.Payload["color"].(string)
					effect, _ := cmd.Payload["effect"].(string)
					setStationLed(color, effect)
				}
			}
		}

		time.Sleep(1200 * time.Millisecond)
	}
}
