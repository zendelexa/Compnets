package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)

// Прототип. Ранее не писал такие приложения,
// по возможности буду рефакторить
// TODO: Рефакторить

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
	User_wss map[*websocket.Conn]bool
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
	RETRIEVAL   = "retrieval"
)

func main() {
	chats = append(chats, Chat{
		Chat_id:  0,
		User_wss: make(map[*websocket.Conn]bool),
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

	chats[0].User_wss[ws] = true
	for _, msg := range chats[0].Messages {
		bytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Ошибка восстановления чата: %v", err)
		}
		msg_retrieval := Event{
			Event_type: NEW_MESSAGE,
			Sender_uid: msg.Sender.Uid,
			Data:       string(bytes),
		}
		websockets[user.Uid].WriteJSON(msg_retrieval)
	}

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
			for user_ws, _ := range chat.User_wss {
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
			if new_chat_id == -1 {
				is_new_chat = true
				new_chat_id = len(chats)
				chats = append(chats, Chat{
					Chat_id:  new_chat_id,
					User_wss: map[*websocket.Conn]bool{websockets[event.Sender_uid]: true},
				})
			}
			chats[new_chat_id].User_wss[websockets[invitation.User_id]] = true

			response := Event{
				Event_type: INVITATION,
				Data:       strconv.Itoa(new_chat_id),
			}

			if is_new_chat {
				websockets[event.Sender_uid].WriteJSON(response)
				for _, msg := range chats[new_chat_id].Messages {
					bytes, err := json.Marshal(msg)
					if err != nil {
						log.Printf("Ошибка восстановления чата: %v", err)
					}
					msg_retrieval := Event{
						Event_type: NEW_MESSAGE,
						Sender_uid: msg.Sender.Uid,
						Data:       string(bytes),
					}
					websockets[event.Sender_uid].WriteJSON(msg_retrieval)
				}
			}
			websockets[invitation.User_id].WriteJSON(response)
			for _, msg := range chats[new_chat_id].Messages {
				bytes, err := json.Marshal(msg)
				if err != nil {
					log.Printf("Ошибка восстановления чата: %v", err)
				}
				msg_retrieval := Event{
					Event_type: NEW_MESSAGE,
					Sender_uid: msg.Sender.Uid,
					Data:       string(bytes),
				}
				websockets[invitation.User_id].WriteJSON(msg_retrieval)
			}
		}
	}
}
