package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
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

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
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
	rand.Read(mask)
	frame = append(frame, mask...)

	masked := make([]byte, l)
	for i := 0; i < l; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame = append(frame, masked...)

	_, err := conn.Write(frame)
	return err
}

func getRealVolume() int {
	cmd := exec.Command("/bin/sh", "-c", "LD_LIBRARY_PATH=/system/vendor/quasar/libs /system/workdir/bin/amixer sget 'Master'")
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

	// 1. Set ALSA mixer
	exec.Command("/system/vendor/quasar/set_volume.sh", strconv.Itoa(percent)).Run()

	// 2. Set Spotify Connect volume
	exec.Command("/data/opt/spotify-connect/cli.sh", "volume", strconv.Itoa(percent)).Run()

	// 3. Update Glagol if token is present
	if token != "" {
		volFloat := float64(percent) / 100.0
		sendGlagolSimple(token, GlagolPayload{
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
	rand.Read(keyBytes)
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

	// Answer ping if any
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil {
			break
		}
		opcode := header[0] & 0x0F
		length := int(header[1] & 0x7F)
		if length == 126 {
			ext := make([]byte, 2)
			io.ReadFull(reader, ext)
			length = int(ext[0])<<8 | int(ext[1])
		}
		p := make([]byte, length)
		io.ReadFull(reader, p)

		if opcode == 0x9 {
			sendWSFrame(conn, 0xA, p)
			break
		}
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

func main() {
	serverURL := flag.String("server", "http://195.62.49.84:8088", "OnlyTyan server base URL")
	flag.Parse()

	log.Printf("Starting OnlyTyan Native Station Daemon (polling %s)...", *serverURL)
	httpClient := &http.Client{Timeout: 5 * time.Second}

	for {
		curVol := getRealVolume()
		pollURL := fmt.Sprintf("%s/api/station/poll?volume=%d", *serverURL, curVol)

		resp, err := httpClient.Get(pollURL)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		var pollResp StationPollResponse
		err = json.NewDecoder(resp.Body).Decode(&pollResp)
		resp.Body.Close()

		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if pollResp.OK && len(pollResp.Commands) > 0 {
			token := pollResp.GlagolToken
			for _, cmd := range pollResp.Commands {
				switch cmd.Type {
				case "say":
					text, _ := cmd.Payload["text"].(string)
					if text != "" && token != "" {
						if err := speakViaGlagol(token, text); err != nil {
							log.Printf("[Glagol Error] %v", err)
						} else {
							log.Printf("[Glagol] Spoke: %s", text)
						}
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
					exec.Command("/data/opt/spotify-connect/cli.sh", "pause").Run()
					if token != "" {
						sendGlagolSimple(token, GlagolPayload{Command: "stop"})
					}
					log.Printf("[Playback] Paused")

				case "resume", "play":
					exec.Command("/data/opt/spotify-connect/cli.sh", "resume").Run()
					if token != "" {
						sendGlagolSimple(token, GlagolPayload{Command: "play"})
					}
					log.Printf("[Playback] Resumed")

				case "next":
					exec.Command("/data/opt/spotify-connect/cli.sh", "next").Run()
					if token != "" {
						sendGlagolSimple(token, GlagolPayload{Command: "next"})
					}
					log.Printf("[Playback] Next track")

				case "prev", "previous":
					exec.Command("/data/opt/spotify-connect/cli.sh", "previous").Run()
					if token != "" {
						sendGlagolSimple(token, GlagolPayload{Command: "prev"})
					}
					log.Printf("[Playback] Previous track")
				}
			}
		}

		time.Sleep(1200 * time.Millisecond)
	}
}
