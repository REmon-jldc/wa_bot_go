package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	googleProto "google.golang.org/protobuf/proto"
)

const TargetGroupID = "120363312348014308@g.us"

var client *whatsmeow.Client
var currentQR string

func isSleepTime() bool {
	now := time.Now()
	hour := now.Hour()
	minute := now.Minute()
	timeInMinutes := hour*60 + minute
	sleepStart := 18*60 + 30
	sleepEnd := 4*60 + 30
	if timeInMinutes >= sleepStart || timeInMinutes < sleepEnd {
		return true
	}
	return false
}

func startAPI() {
	// 🌟 பின்னணியில் Status செக் செய்யும் API 🌟
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if client != nil && client.IsLoggedIn() {
			json.NewEncoder(w).Encode(map[string]interface{}{"connected": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"connected": false, "qr": currentQR})
	})

	// 🌟 Smart Web Page (Page Reload ஆகாது) 🌟
	http.HandleFunc("/qr", func(w http.ResponseWriter, r *http.Request) {
		html := `
		<html>
		<head>
			<title>WhatsApp Bot Login</title>
			<script src="https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"></script>
			<style>
				body { font-family: sans-serif; background-color: #f0f2f5; display: flex; justify-content: center; align-items: center; height: 100vh; flex-direction: column; }
				.card { background: white; padding: 40px; border-radius: 10px; box-shadow: 0 4px 10px rgba(0,0,0,0.1); text-align: center; }
				#status { font-weight: bold; margin-bottom: 15px; color: #ff9800; font-size: 18px; }
				#qrcode img { margin: 0 auto; }
			</style>
		</head>
		<body>
			<div class="card">
				<h2 style="color:#075e54; margin-bottom:5px;">Scan to Login</h2>
				<div id="status">⏳ Waiting for QR Code...</div>
				<div id="qrcode"></div>
				<p style="margin-top:20px; color:#555;">Open WhatsApp -> Linked Devices -> Scan</p>
			</div>
			<script>
				let currentQRText = "";
				let qrObj = null;

				async function checkStatus() {
					try {
						let res = await fetch('/api/status');
						let data = await res.json();
						
						if (data.connected) {
							document.getElementById("status").innerHTML = "✅ Connected successfully!";
							document.getElementById("status").style.color = "green";
							document.getElementById("qrcode").innerHTML = ""; 
							return; 
						}

						if (data.qr && data.qr !== currentQRText) {
							currentQRText = data.qr;
							document.getElementById("status").innerText = "🔄 Scan the QR Code below";
							document.getElementById("status").style.color = "#075e54";
							
							document.getElementById("qrcode").innerHTML = "";
							qrObj = new QRCode(document.getElementById("qrcode"), {
								text: currentQRText,
								width: 256,
								height: 256,
								colorDark : "#000000",
								colorLight : "#ffffff",
								correctLevel : QRCode.CorrectLevel.H
							});
						}
					} catch (e) {
						document.getElementById("status").innerText = "⚠️ Connecting to server...";
					}
					setTimeout(checkStatus, 2000); 
				}
				checkStatus();
			</script>
		</body>
		</html>
		`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	http.HandleFunc("/send_reply", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)

		groupID := req["group_id"]
		replyText := req["reply"]

		go func() {
			ctx := context.Background()
			targetJID, _ := types.ParseJID(groupID)

			client.SendChatPresence(ctx, targetJID, types.ChatPresenceComposing, types.ChatPresenceMediaText)
			typingDelay := time.Duration(rand.Intn(6)+3) * time.Second
			time.Sleep(typingDelay)
			client.SendChatPresence(ctx, targetJID, types.ChatPresencePaused, types.ChatPresenceMediaText)

			client.SendMessage(ctx, targetJID, &proto.Message{
				Conversation: googleProto.String(replyText),
			})
			fmt.Println("✅ Reply Sent to WhatsApp!")
		}()
		w.WriteHeader(200)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println("🌐 Go API Server running on port:", port)
	http.ListenAndServe(":"+port, nil)
}

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe || v.Message.GetConversation() == "" {
			return
		}
		if v.Info.MessageSource.Chat.String() != TargetGroupID {
			return
		}
		if isSleepTime() {
			return
		}

		go func() {
			ctx := context.Background()

			readDelay := time.Duration(rand.Intn(6)+3) * time.Second
			time.Sleep(readDelay)
			client.MarkRead(ctx, []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.MessageSource.Chat, v.Info.Sender, types.ReceiptTypeRead)
			fmt.Println("👀 Blue Tick Sent!")

			payload := map[string]interface{}{
				"id":        int(time.Now().Unix()),
				"sender":    v.Info.Sender.User,
				"message":   v.Message.GetConversation(),
				"push_name": v.Info.PushName,
				"timestamp": v.Info.Timestamp.Format(time.RFC3339),
				"group_id":  v.Info.MessageSource.Chat.String(),
			}
			jsonData, _ := json.Marshal(payload)

			http.Post("https://remon1810.pythonanywhere.com/webhook", "application/json", bytes.NewBuffer(jsonData))
			fmt.Println("☁️ Saved securely to Cloud Database!")
		}()
	}
}

func main() {
	go startAPI()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("❌ DATABASE_URL is not set.")
		return
	}

	ctx := context.Background()
	container, err := sqlstore.New(ctx, "postgres", dbURL, waLog.Stdout("Database", "WARN", true))
	if err != nil {
		panic(err)
	}

	deviceStore, _ := container.GetFirstDevice(ctx)
	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "WARN", true))
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(ctx)
		client.Connect()
		for evt := range qrChan {
			if evt.Event == "code" {
				currentQR = evt.Code
				fmt.Println("🚀 New QR Code generated by WhatsApp.")
			}
		}
	} else {
		client.Connect()
		fmt.Println("✅ Connected to WhatsApp!")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}
