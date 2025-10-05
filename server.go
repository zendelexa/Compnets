package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/websocket"
)

// Прототип. Ранее не писал такие приложения,
// по возможности буду рефакторить
// TODO: Рефакторить

type User struct {
	Uid       int             `json:"uid"`
	Username  string          `json:"username"`
	Websocket *websocket.Conn `json:"-"`
	Chat_ids  map[int]bool    `json:"-"`
}

// Клиенты и каналы для обмена сообщениями
var clients = make(map[int]User)
var websockets = make(map[int]*websocket.Conn)
var events = make(chan Event)
var chats []Chat
var usernames = make(map[int]string)
var user_ids = make(map[string]int) // TODO: избавиться,
// чтобы была возможность делать пользователей с одинаковыми именами
var passwords = make(map[int]string)
var coedits []Coedit

type Authentification struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	Is_registrating bool   `json:"is_registrating"`
}

// Настройки апгрейда WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // В продакшене нужно проверять origin!
	},
}

type CoeditInfo struct {
	Is_coedit bool `json:"is_coedit"`
	Coedit_id int  `json:"coedit_id"`
}

// Структура для сообщений
type Message struct {
	Sender      User       `json:"sender"`
	Message     string     `json:"message"`
	Chat_id     int        `json:"chat_id"`
	Filename    string     `json:"filename"`
	Coedit_info CoeditInfo `json:"coedit_info"`
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
	Chat_id    int `json:"chat_id"`
	User_infos map[int]ChatUserInfo
	Messages   []Message
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

type VersionWithContent struct {
	Version int    `json:"version"`
	Content string `json:"content"`
}

type CoeditVersion struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

type Coedit struct {
	Chat_id               int                  `json:"chat_id"`
	Coedit_id             int                  `json:"coedit_id"`
	Versions_with_content []VersionWithContent `json:"-"`
	Versions              []CoeditVersion      `json:"versions"`
	ActualVersion         VersionWithContent   `json:"version"`
}

type AddCoedit struct {
	Username  string `json:"username"`
	Chat_id   int    `json:"chat_id"`
	Coedit_id int    `json:"coedit_id"`
}

type SyncCoedit struct {
	Coedit_id int `json:"coedit_id"`
	VersionWithContent
}

func newCoedit(chat_id int) Coedit {
	result := Coedit{
		Chat_id:   chat_id,
		Coedit_id: len(coedits),
		ActualVersion: VersionWithContent{
			Version: 0,
			Content: "",
		},
	}
	result.Versions_with_content = append(result.Versions_with_content, result.ActualVersion)
	result.Versions = append(result.Versions, CoeditVersion{
		Number: 0,
		Name:   "Initial version",
	})
	result.ActualVersion.Version = 1
	return result
}

func (coedit *Coedit) makeSave(name string) {
	coedit.Versions_with_content = append(coedit.Versions_with_content, coedit.ActualVersion)
	coedit.Versions = append(coedit.Versions, CoeditVersion{
		Number: coedit.ActualVersion.Version,
		Name:   name,
	})
	log.Printf("Создана версия %d: \"%s\"", coedit.ActualVersion.Version, coedit.ActualVersion.Content)
	coedit.ActualVersion.Version++
}

func (coedit *Coedit) revert(version int) {
	coedit.ActualVersion = coedit.Versions_with_content[version]
}

type SaveCoedit struct {
	Coedit_id int    `json:"coedit_id"`
	Name      string `json:"name"`
}

type RevertCoedit struct {
	Coedit_id int `json:"coedit_id"`
	Version   int `json:"version"`
}

const (
	NEW_MESSAGE      = "message"
	INVITATION       = "invitation"
	GET_UID          = "uid"
	RETRIEVAL        = "retrieval"
	FILE             = "file"
	ADDITION         = "addition"
	TOGGLE_USER_INFO = "toggle_user_info"
	ADD_COEDIT       = "add_coedit"
	SYNC_COEDIT      = "sync_coedit"
	SAVE_COEDIT      = "save_coedit"
	REVERT_COEDIT    = "revert_coedit"
)

func main() {
	chats = append(chats, Chat{
		Chat_id:    0,
		User_infos: make(map[int]ChatUserInfo),
	})

	// Статические файлы (наш HTML/JS клиент)
	// fs := http.FileServer(http.Dir("./static"))
	// http.Handle("/", fs)

	http.HandleFunc("/auth", handleAuth)

	// WebSocket endpoint
	http.HandleFunc("/ws/", handleConnections)

	// TODO: переделать папки так, чтобы был только один общий FileServer
	chat_fs := http.FileServer(http.Dir("./static"))
	http.Handle("/chat", http.StripPrefix("/chat", chat_fs))

	fs := http.FileServer(http.Dir("./auth"))
	http.Handle("/", fs)

	// Запускаем горутину для обработки сообщений
	go handleEvents()

	// Запускаем сервер
	log.Println("Сервер запущен на :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("Ошибка сервера:", err)
	}
}

type AuthResponse struct {
	Is_successful bool   `json:"is_successful"`
	Text          string `json:"text"`
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var auth_data Authentification
		json.NewDecoder(r.Body).Decode(&auth_data)

		fmt.Printf("Auth attempt %s %s\n", auth_data.Username, auth_data.Password)

		is_ok := false

		user_id := -1
		if auth_data.Is_registrating {
			user_id = len(usernames)
			usernames[user_id] = auth_data.Username
			user_ids[auth_data.Username] = user_id
			passwords[user_id] = auth_data.Password

			clients[user_id] = User{
				Uid:      user_id,
				Username: auth_data.Username,
				Chat_ids: make(map[int]bool),
			}
			is_ok = true
		} else {
			// TODO: вынести в отдельную функцию и переделать в if return
			var is_registered bool
			user_id, is_registered = user_ids[auth_data.Username]
			if is_registered {
				if auth_data.Password == passwords[user_id] {
					is_ok = true

				}
			}
		}

		if is_ok {
			fmt.Print("Auth completed\n")
			// page_data, err := os.ReadFile("./static/index.html")
			// if err != nil {
			// 	log.Fatal(err)
			// }

			// w.Header().Set("Content-Type", "text/html")
			// fmt.Fprint(w, string(page_data))

			http.SetCookie(w, &http.Cookie{
				Name:     "user_id",
				Value:    strconv.Itoa(user_id),
				Path:     "/",
				HttpOnly: true,
			})

			response := AuthResponse{
				Is_successful: true,
				Text:          "Auth successful",
			}
			data, err := json.Marshal(response)
			if err != nil {
				log.Fatal(err)
			}
			w.Write(data)
		} else {
			fmt.Print("Auth failed\n")
			response := AuthResponse{
				Is_successful: false,
				Text:          "Auth failed",
			}
			data, err := json.Marshal(response)
			if err != nil {
				log.Fatal(err)
			}
			w.Write(data)
		}
	}
}

// var new_clinet_id int = 0

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Апгрейд HTTP соединения до WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	cookie, err := r.Cookie("user_id")
	if err != nil {
		log.Printf("Ошибка получения куки")
		return
		// TODO: добавить переадресацию на страницу регистрации?
	}

	user_id, _ := strconv.Atoi(cookie.Value)

	username := usernames[user_id]

	// user := User{
	// 	Uid:       user_id,
	// 	Username:  username,
	// 	Websocket: ws,
	// }
	// new_clinet_id++
	// Регистрируем нового клиента
	// clients[user.Uid] = user

	user := clients[user_id]
	user.Websocket = ws
	clients[user_id] = user

	websockets[user.Uid] = ws

	event := Event{
		Event_type: GET_UID,
		Data:       strconv.Itoa(user.Uid),
	}
	ws.WriteJSON(event)
	log.Printf("Имя пользователя: ")
	log.Print(username)
	log.Printf(" с uid=%d\n", user.Uid)

	// addUserToChat(0, user.Uid, false, true)
	log.Printf("У пользователя %d обнаружено %d чатов\n", user_id, len(clients[user_id].Chat_ids))
	for chat_id := range clients[user_id].Chat_ids {
		log.Printf("У пользователя %d обнаружен чат %d\n", user_id, chat_id)
		addUserToChat(chat_id, user.Uid, chats[chat_id].User_infos[user_id].Is_admin, false)
	}

	for {
		var event Event

		// Читаем новое сообщение от клиента
		err := ws.ReadJSON(&event)
		if err != nil {
			log.Printf("Ошибка чтения события: %v", err)
			delete(websockets, user.Uid)
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

			if chats[msg.Chat_id].User_infos[msg.Sender.Uid].Is_muted {
				continue
			}

			msg.Sender.Username = usernames[msg.Sender.Uid]
			data, _ := json.Marshal(msg)
			event.Data = string(data)

			chat := &chats[msg.Chat_id]
			chat.Messages = append(chat.Messages, msg)

			log.Printf("Сообщение от %s в чате с ID %d: %s\n", msg.Sender.Username, msg.Chat_id, msg.Message)

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
			}

			NotifyChat(msg.Chat_id, event)

		case INVITATION:
			var invitation Invitation
			err := json.Unmarshal([]byte(event.Data), &invitation)
			if err != nil {
				log.Printf("Ошибка чтения приглашения: %v", err)
				return
			}

			if invitation.User_id == event.Sender_uid {
				continue
			}

			new_chat_id := invitation.Chat_id
			is_new_chat := false
			if new_chat_id == -1 {
				is_new_chat = true
				new_chat_id = len(chats)
				chats = append(chats, Chat{
					Chat_id:    new_chat_id,
					User_infos: make(map[int]ChatUserInfo),
				})
			}

			if is_new_chat {
				addUserToChat(new_chat_id, event.Sender_uid, true, true)
			}
			addUserToChat(new_chat_id, invitation.User_id, false, true)

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

			if !chats[data.Chat_id].User_infos[event.Sender_uid].Is_admin {
				continue
			}

			NotifyChat(data.Chat_id, event)

			if data.Is_admin {
				chat_user_info := chats[data.Chat_id].User_infos[data.User_id]
				chat_user_info.Is_admin = !chat_user_info.Is_admin
				chats[data.Chat_id].User_infos[data.User_id] = chat_user_info
				// log.Printf("Is_admin: %b", chats[data.Chat_id].User_wss[data.User_id].Is_admin)
			}
			if data.Is_muted {
				chat_user_info := chats[data.Chat_id].User_infos[data.User_id]
				chat_user_info.Is_muted = !chat_user_info.Is_muted
				chats[data.Chat_id].User_infos[data.User_id] = chat_user_info
				// log.Printf("Is_muted: %b", chats[data.Chat_id].User_wss[data.User_id].Is_muted)
			}
			if data.Is_kicked {
				delete(chats[data.Chat_id].User_infos, data.User_id)
			}
		case ADD_COEDIT:
			var data struct {
				Chat_id int `json:"chat_id"`
			}
			err := json.Unmarshal([]byte(event.Data), &data)
			if err != nil {
				log.Printf("Ошибка чтения добавления коэдита: %v", err)
			}

			coedits = append(coedits, newCoedit(data.Chat_id))

			msg := Message{
				Sender:  clients[event.Sender_uid],
				Chat_id: data.Chat_id,
				Coedit_info: CoeditInfo{
					Is_coedit: true,
					Coedit_id: coedits[len(coedits)-1].Coedit_id,
				},
				Message:  "",
				Filename: "",
			}

			response_data, err := json.Marshal(msg)

			if err != nil {
				log.Printf("Ошибка создания коэдита")
			}

			response := Event{
				Event_type: NEW_MESSAGE,
				Sender_uid: event.Sender_uid,
				Data:       string(response_data),
			}

			chat := &chats[msg.Chat_id]
			chat.Messages = append(chat.Messages, msg)

			NotifyChat(data.Chat_id, response)

			log.Printf("В чате %d создан коэдит %d пользователем %d", data.Chat_id, msg.Coedit_info.Coedit_id, event.Sender_uid)
		case SYNC_COEDIT:
			log.Print("Attempt to sync")

			var data SyncCoedit
			err := json.Unmarshal([]byte(event.Data), &data)
			if err != nil {
				log.Printf("Ошибка чтения при попытке синхронизации коэдита")
			}

			if data.Coedit_id == -1 {
				continue
			}

			if data.Version == -1 {
				NotifyUser(event.Sender_uid, WrapCoeditInEvent(data.Coedit_id))
			} else {
				coedits[data.Coedit_id].ActualVersion.Content = data.Content

				NotifyChat(coedits[data.Coedit_id].Chat_id, WrapCoeditInEvent(data.Coedit_id))
			}
		case SAVE_COEDIT:
			var data SaveCoedit
			err := json.Unmarshal([]byte(event.Data), &data)
			if err != nil {
				log.Printf("Ошибка чтения при попытке сохранения коэдита")
			}

			coedits[data.Coedit_id].makeSave(data.Name)

			NotifyChat(coedits[data.Coedit_id].Chat_id, WrapCoeditInEvent(data.Coedit_id))
		case REVERT_COEDIT:
			var data RevertCoedit
			err := json.Unmarshal([]byte(event.Data), &data)
			if err != nil {
				log.Printf("Ошибка чтения при попытке возврата коэдита")
			}

			log.Printf("ПОПЫТКА возврата к %d", data.Version)
			coedits[data.Coedit_id].revert(data.Version)
			log.Printf("возврат к %d", coedits[data.Coedit_id].ActualVersion.Version)

			NotifyChat(coedits[data.Coedit_id].Chat_id, WrapCoeditInEvent(data.Coedit_id))
		}
	}
}

func addUserToChat(chat_id int, user_id int, is_admin bool, do_notify_others bool) {
	if _, is_in_chat := chats[chat_id].User_infos[user_id]; !is_in_chat {
		chats[chat_id].User_infos[user_id] = ChatUserInfo{
			Is_admin: is_admin,
			Is_muted: false,
		}
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
		Username: clients[user_id].Username,
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

	for other_user_id := range chats[chat_id].User_infos {
		if do_notify_others && other_user_id != user_id {
			other_user_ws, is_online := websockets[other_user_id]
			if is_online {
				other_user_ws.WriteJSON(response)
			}
		}

		addition2 := Addition{
			Chat_id:  chat_id,
			User_id:  clients[other_user_id].Uid,
			Username: clients[other_user_id].Username,                   // TODO: create new map
			Is_admin: chats[chat_id].User_infos[other_user_id].Is_admin, // FIXME
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

	clients[user_id].Chat_ids[chat_id] = true
	log.Printf("Пользователь %d добавлен в чат %d\n", user_id, chat_id)

}

func NotifyChat(chat_id int, event Event) {
	for user_id := range chats[chat_id].User_infos {
		NotifyUser(user_id, event)
	}
}

func NotifyUser(user_id int, event Event) {
	user_ws, is_online := websockets[user_id]
	if is_online {
		err := user_ws.WriteJSON(event)
		if err != nil {
			log.Printf("Ошибка записи: %v", err)
			// Зачем удалять пользователя?
			user_ws.Close()
			delete(websockets, user_id)
		}
	}
}

func WrapCoeditInEvent(coedit_id int) Event {
	response_data, err := json.Marshal(coedits[coedit_id])
	if err != nil {
		log.Printf("Ошибка при сборе данных о коэдите")
	}

	return Event{
		Event_type: SYNC_COEDIT,
		Sender_uid: -1,
		Data:       string(response_data),
	}
}
