package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
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
	Sender   User   `json:"sender"`
	Message  string `json:"message"`
	Chat_id  int    `json:"chat_id"`
	Filename string `json:"filename"`
}

type File struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
}

type ChatUserInfo struct {
	Is_admin bool `json:"is_admin"`
	Is_muted bool `json:"is_muted"`
}

type ToggleUserInfo struct {
	User_id   int  `json:"user_id"`
	Chat_id   int  `json:"chat_id"`
	Is_admin  bool `json:"is_admin"`
	Is_muted  bool `json:"is_muted"`
	Is_kicked bool `json:"is_kicked"`
}

type Chat struct {
	Chat_id  int `json:"chat_id"`
	User_wss map[*websocket.Conn]ChatUserInfo
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

type Addition struct {
	Chat_id  int    `json:"chat_id"`
	User_id  int    `json:"user_id"`
	Username string `json:"username"`
	Is_admin bool   `json:"is_admin"`
}

const (
	NEW_MESSAGE      = "message"
	INVITATION       = "invitation"
	GET_UID          = "uid"
	RETRIEVAL        = "retrieval"
	FILE             = "file"
	ADDITION         = "addition"
	TOGGLE_USER_INFO = "toggle_user_info"
)

func main() {
	chats = append(chats, Chat{
		Chat_id:  0,
		User_wss: make(map[*websocket.Conn]ChatUserInfo),
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

	addUserToChat(0, user.Uid, false)

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

			if chats[msg.Chat_id].User_wss[websockets[msg.Sender.Uid]].Is_muted {
				continue
			}

			chat := &chats[msg.Chat_id]
			chat.Messages = append(chat.Messages, msg)

			if msg.Filename != "" {
				err = os.WriteFile("attachments/"+msg.Filename, []byte(msg.Message), 0644)
				if err != nil {
					log.Fatal(err)
				}

				msg.Message = ""
				data, err := json.Marshal(msg)
				if err != nil {
					log.Fatal(err)
				}
				event.Data = string(data)
				for user_ws := range chat.User_wss {
					err := user_ws.WriteJSON(event)
					if err != nil {
						log.Printf("Ошибка записи: %v", err)
						user_ws.Close()
						delete(clients, user_ws)
					}
				}
			} else {
				for user_ws := range chat.User_wss {
					err := user_ws.WriteJSON(event)
					if err != nil {
						log.Printf("Ошибка записи: %v", err)
						user_ws.Close()
						delete(clients, user_ws)
					}
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
					User_wss: make(map[*websocket.Conn]ChatUserInfo),
				})
			}

			if is_new_chat {
				addUserToChat(new_chat_id, event.Sender_uid, true)
			}
			addUserToChat(new_chat_id, invitation.User_id, false)

		case FILE:
			data, err := os.ReadFile("attachments/" + event.Data)
			if err != nil {
				log.Fatal(err)
			}

			file := File{
				Content:  string(data),
				Filename: event.Data,
			}

			data, err = json.Marshal(file)
			if err != nil {
				log.Fatal(err)
			}

			response := Event{
				Event_type: FILE,
				Data:       string(data),
			}

			websockets[event.Sender_uid].WriteJSON(response)

		case TOGGLE_USER_INFO:
			var data ToggleUserInfo
			err := json.Unmarshal([]byte(event.Data), &data)
			if err != nil {
				log.Fatal(err)
			}

			if !chats[data.Chat_id].User_wss[websockets[event.Sender_uid]].Is_admin {
				continue
			}

			for user_ws := range chats[data.Chat_id].User_wss {
				err := user_ws.WriteJSON(event)
				if err != nil {
					log.Printf("Ошибка записи: %v", err)
					user_ws.Close()
					delete(clients, user_ws)
				}
			}

			if data.Is_admin {
				chat_user_info := chats[data.Chat_id].User_wss[websockets[data.User_id]]
				chat_user_info.Is_admin = !chat_user_info.Is_admin
				chats[data.Chat_id].User_wss[websockets[data.User_id]] = chat_user_info
				// log.Printf("Is_admin: %b", chats[data.Chat_id].User_wss[websockets[data.User_id]].Is_admin)
			}
			if data.Is_muted {
				chat_user_info := chats[data.Chat_id].User_wss[websockets[data.User_id]]
				chat_user_info.Is_muted = !chat_user_info.Is_muted
				chats[data.Chat_id].User_wss[websockets[data.User_id]] = chat_user_info
				// log.Printf("Is_muted: %b", chats[data.Chat_id].User_wss[websockets[data.User_id]].Is_muted)
			}
			if data.Is_kicked {
				delete(chats[data.Chat_id].User_wss, websockets[data.User_id])
			}
		}
	}
}

func addUserToChat(chat_id int, user_id int, is_admin bool) {
	chats[chat_id].User_wss[websockets[user_id]] = ChatUserInfo{
		Is_admin: is_admin,
		Is_muted: false,
	}

	response := Event{
		Event_type: INVITATION,
		Data:       strconv.Itoa(chat_id),
	}

	websockets[user_id].WriteJSON(response)
	for _, msg := range chats[chat_id].Messages {
		bytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("Ошибка восстановления чата: %v", err)
		}
		msg_retrieval := Event{
			Event_type: NEW_MESSAGE,
			Sender_uid: msg.Sender.Uid,
			Data:       string(bytes),
		}
		websockets[user_id].WriteJSON(msg_retrieval)
	}

	addition := Addition{
		Chat_id:  chat_id,
		User_id:  user_id,
		Username: clients[websockets[user_id]].Username, // TODO: create new map
		Is_admin: is_admin,
	}

	data, err := json.Marshal(addition)
	if err != nil {
		log.Fatal(err)
	}

	response = Event{
		Event_type: ADDITION,
		Data:       string(data),
	}

	for user_ws := range chats[chat_id].User_wss {
		user_ws.WriteJSON(response)

		if user_ws == websockets[user_id] {
			continue
		}
		addition2 := Addition{
			Chat_id:  chat_id,
			User_id:  clients[user_ws].Uid,
			Username: clients[user_ws].Username,                 // TODO: create new map
			Is_admin: chats[chat_id].User_wss[user_ws].Is_admin, // FIXME
		}

		data, err := json.Marshal(addition2)
		if err != nil {
			log.Fatal(err)
		}

		response2 := Event{
			Event_type: ADDITION,
			Data:       string(data),
		}

		websockets[user_id].WriteJSON(response2)
	}
}
