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
var broadcast = make(chan Message)

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
}

type Event struct {
	Event_type string `json:"event_type"`
	Data       string `json:"data"`
}

const (
	NEW_MESSAGE = "message"
	INVITATION  = "invitation"
	GET_UID     = "uid"
)

func main() {
	// Статические файлы (наш HTML/JS клиент)
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// WebSocket endpoint
	http.HandleFunc("/ws/", handleConnections)

	// Запускаем горутину для обработки сообщений
	go handleMessages()

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

	event := Event{
		Event_type: GET_UID,
		Data:       strconv.Itoa(user.Uid),
	}
	ws.WriteJSON(event)
	log.Printf("Имя пользователя: ")
	log.Print(username)
	log.Printf(" с uid=%d\n", user.Uid)

	for {
		var msg Message
		// Читаем новое сообщение от клиента
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Ошибка чтения: %v", err)
			delete(clients, ws)
			break
		}
		msg.Sender = user

		// Отправляем сообщение в broadcast канал
		broadcast <- msg
	}
}

func handleMessages() {
	for {
		// Достаем сообщение из канала
		msg := <-broadcast

		msg_json, err := json.Marshal(msg)
		if err != nil {
			panic(err)
		}
		event := Event{
			Event_type: NEW_MESSAGE,
			Data:       string(msg_json),
		}

		// Рассылаем всем подключенным клиентам
		for client := range clients {
			err := client.WriteJSON(event)
			if err != nil {
				log.Printf("Ошибка записи: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}
