package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/seedmux/relay/sdproto"
)

func main() {
	url := "ws://127.0.0.1:8080"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	sizeKB := 2650 // 2.65MB default
	if len(os.Args) > 2 {
		sizeKB, _ = strconv.Atoi(os.Args[2])
	}
	rounds := 5
	if len(os.Args) > 3 {
		rounds, _ = strconv.Atoi(os.Args[3])
	}

	ctx := context.Background()
	payload := make([]byte, sizeKB*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Fatalf("rand.Read: %v", err)
	}

	fmt.Printf("relay: %s\n", url)
	fmt.Printf("payload: %d KB\n", sizeKB)
	fmt.Printf("rounds: %d\n\n", rounds)

	token := fmt.Sprintf("bench-%d", time.Now().UnixNano())

	var wg sync.WaitGroup
	wg.Add(2)

	var recvTimes []time.Duration

	go func() {
		defer wg.Done()
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			log.Fatalf("receiver dial: %v", err)
		}
		conn.SetReadLimit(64 * 1024 * 1024)
		defer conn.CloseNow()

		writeJSONFrame(ctx, conn, sdproto.MsgAuth, map[string]string{
			"type":      "auth",
			"token":     token,
			"role":      "desktop",
			"publicKey": "",
			"deviceId":  "bench-desktop",
		})

		waitForPaired(ctx, conn, "receiver")

		for i := 0; i < rounds; i++ {
			t0 := time.Now()
			_, data, err := conn.Read(ctx)
			if err != nil {
				log.Fatalf("receiver read: %v", err)
			}
			elapsed := time.Since(t0)
			recvTimes = append(recvTimes, elapsed)
			mbps := float64(len(data)) / 1024 / 1024 / elapsed.Seconds()
			fmt.Printf("[recv #%d] %d KB in %v (%.1f MB/s)\n", i+1, len(data)/1024, elapsed, mbps)
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(500 * time.Millisecond)

		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			log.Fatalf("sender dial: %v", err)
		}
		conn.SetReadLimit(64 * 1024 * 1024)
		defer conn.CloseNow()

		writeJSONFrame(ctx, conn, sdproto.MsgAuth, map[string]string{
			"type":      "auth",
			"token":     token,
			"role":      "mobile",
			"publicKey": "",
			"deviceId":  "bench-mobile",
		})

		waitForPaired(ctx, conn, "sender")
		time.Sleep(200 * time.Millisecond)

		wire := sdproto.Pack(sdproto.MsgData, 0, payload)
		for i := 0; i < rounds; i++ {
			t0 := time.Now()
			if err := conn.Write(ctx, websocket.MessageBinary, wire); err != nil {
				log.Fatalf("sender write: %v", err)
			}
			writeMs := time.Since(t0).Seconds() * 1000
			fmt.Printf("[send #%d] %d KB write=%.1fms\n", i+1, sizeKB, writeMs)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	wg.Wait()

	if len(recvTimes) > 0 {
		fmt.Println("\n--- Summary ---")
		var total time.Duration
		for _, d := range recvTimes {
			total += d
		}
		avg := total / time.Duration(len(recvTimes))
		avgMBps := float64(sizeKB) / 1024 / avg.Seconds()
		fmt.Printf("avg recv: %v (%.1f MB/s)\n", avg, avgMBps)
	}
}

func writeJSONFrame(ctx context.Context, conn *websocket.Conn, msgType byte, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("marshal frame 0x%02x: %v", msgType, err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, sdproto.Pack(msgType, 0, data)); err != nil {
		log.Fatalf("write frame 0x%02x: %v", msgType, err)
	}
}

func waitForPaired(ctx context.Context, conn *websocket.Conn, label string) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			log.Fatalf("%s read: %v", label, err)
		}
		frame, err := sdproto.Unpack(data)
		if err != nil {
			log.Fatalf("%s unpack: %v", label, err)
		}
		if frame.MsgType == sdproto.MsgPaired {
			return
		}
	}
}
