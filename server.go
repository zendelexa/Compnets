package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)

type User struct {
	Uid       int    `json:"uid"`
	Username  string `json:"username"`
	Websocket *websocket.Conn
}

// Клиенты и каналы для обмена сообщениями
var clients = make(map[*websocket.Conn]User)
var websockets = make(map[int]*websocket.Conn)
var events = make(chan Event)
var chats []Chat

// Настройки апгрейда WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // В продакшене нужно проверять origin!
	},
}

// Структура для сообщений
type Message struct {
	Sender  User   `json:"sender"`
	Message string `json:"message"`
	Chat_id int    `json:"chat_id"`
}

type Chat struct {
	Chat_id  int `json:"chat_id"`
	User_wss []*websocket.Conn
	Messages []Message
}

type Event struct {
	Event_type string `json:"event_type"`
	Sender_uid int    `json:"sender_uid"`
	Data       string `json:"data"`
}

type Invitation struct {
	Chat_id int `json:"chat_id"`
	User_id int `json:"user_id"`
}

const (
	NEW_MESSAGE = "message"
	INVITATION  = "invitation"
	GET_UID     = "uid"
)

func main() {
	chats = append(chats, Chat{
		Chat_id: 0,
	})

	// Статические файлы (наш HTML/JS клиент)
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// WebSocket endpoint
	http.HandleFunc("/ws/", handleConnections)

	// Запускаем горутину для обработки сообщений
	go handleEvents()

	// Запускаем сервер
	log.Println("Сервер запущен на :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("Ошибка сервера:", err)
	}
}

var new_clinet_id int = 0

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Апгрейд HTTP соединения до WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	username := strings.TrimPrefix(r.URL.Path, "/ws/")

	user := User{
		Uid:       new_clinet_id,
		Username:  username,
		Websocket: ws,
	}
	new_clinet_id++
	// Регистрируем нового клиента
	clients[ws] = user
	websockets[user.Uid] = ws
	chats[0].User_wss = append(chats[0].User_wss, ws)

	event := Event{
		Event_type: GET_UID,
		Data:       strconv.Itoa(user.Uid),
	}
	ws.WriteJSON(event)
	log.Printf("Имя пользователя: ")
	log.Print(username)
	log.Printf(" с uid=%d\n", user.Uid)

	event = Event{
		Event_type: INVITATION,
		Data:       "0",
	}
	ws.WriteJSON(event)

	for {
		var event Event

		// Читаем новое сообщение от клиента
		err := ws.ReadJSON(&event)
		if err != nil {
			log.Printf("Ошибка чтения события: %v", err)
			delete(clients, ws)
			break
		}
		// Передаём событие горутине
		events <- event
	}
}

func handleEvents() {
	for {
		event := <-events

		// Заменить на switch
		switch event.Event_type {
		case NEW_MESSAGE:
			var msg Message
			err := json.Unmarshal([]byte(event.Data), &msg)
			if err != nil {
				log.Printf("Ошибка чтения сообщения: %v", err)
			}
			chat := &chats[msg.Chat_id]
			chat.Messages = append(chat.Messages, msg)
			for _, user_ws := range chat.User_wss {
				err := user_ws.WriteJSON(event)
				if err != nil {
					log.Printf("Ошибка записи: %v", err)
					user_ws.Close()
					delete(clients, user_ws)
				}
			}
		case INVITATION:
			var invitation Invitation
			err := json.Unmarshal([]byte(event.Data), &invitation)
			if err != nil {
				log.Printf("Ошибка чтения приглашения: %v", err)
				return
			}

			new_chat_id := invitation.Chat_id
			is_new_chat := false
			log.Printf("DBG: %d", new_chat_id)
			log.Printf("DBG: %d %d", event.Sender_uid, invitation.User_id)
			if new_chat_id == -1 {
				is_new_chat = true
				new_chat_id = len(chats)
				chats = append(chats, Chat{
					Chat_id:  new_chat_id,
					User_wss: []*websocket.Conn{websockets[event.Sender_uid]},
				})
			}
			chats[new_chat_id].User_wss = append(chats[new_chat_id].User_wss, websockets[invitation.User_id])

			response := Event{
				Event_type: INVITATION,
				Data:       strconv.Itoa(new_chat_id),
			}

			if is_new_chat {
				websockets[event.Sender_uid].WriteJSON(response)
			}
			websockets[invitation.User_id].WriteJSON(response)
		}
	}
}
