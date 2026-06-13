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

	_ "github.com/lib/pq" // 🌟 Render Postgres-க்கான லைப்ரரி 🌟
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	googleProto "google.golang.org/protobuf/proto"
)

// 🌟 உங்கள் குரூப் ஐடி 🌟
const TargetGroupID = "120363312348014308@g.us"

var client *whatsmeow.Client

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

// 🌟 லேப்டாப்பில் இருந்து வரும் பதிலை வாங்கும் API Server 🌟
func startAPI() {
	http.HandleFunc("/send_reply", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)

		groupID := req["group_id"]
		replyText := req["reply"]

		go func() {
			ctx := context.Background()
			targetJID, _ := types.ParseJID(groupID)

			// 🌟 HUMAN LOGIC: ரிப்ளை அனுப்பும் முன் Typing... (3 to 8 Sec) 🌟
			client.SendChatPresence(ctx, targetJID, types.ChatPresenceComposing, types.ChatPresenceMediaText)
			typingDelay := time.Duration(rand.Intn(6)+3)*time.Second
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
		port = "8080" // Render-ல் ஆட்டோமேட்டிக்காக போர்ட் எடுத்துக்கொள்ளும்
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

			// 🌟 HUMAN LOGIC: மெசேஜ் வந்ததும் Blue Tick (3 to 8 Sec) 🌟
			readDelay := time.Duration(rand.Intn(6)+3)*time.Second
			time.Sleep(readDelay)
			client.MarkRead(ctx, []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.MessageSource.Chat, v.Info.Sender, types.ReceiptTypeRead)
			fmt.Println("👀 Blue Tick Sent!")

			// டேட்டாவை PythonAnywhere-ல் சேவ் செய்தல்
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

	// 🌟 Render-ல் இருந்து Database URL-ஐ எடுக்கும் லாஜிக் 🌟
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("❌ DATABASE_URL is not set. Please set it in Render Environment Variables.")
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
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("Scan the QR code above to login!")
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