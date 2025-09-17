package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// Клиенты и каналы для обмена сообщениями
var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan Message)

// Настройки апгрейда WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // В продакшене нужно проверять origin!
	},
}

// Структура для сообщений
type Message struct {
	Username string `json:"username"`
	Message  string `json:"message"`
	Time     string `json:"time"`
}

func main() {
	// Статические файлы (наш HTML/JS клиент)
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// WebSocket endpoint
	http.HandleFunc("/ws", handleConnections)

	// Запускаем горутину для обработки сообщений
	go handleMessages()

	// Запускаем сервер
	log.Println("Сервер запущен на :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("Ошибка сервера:", err)
	}
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Апгрейд HTTP соединения до WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	// Регистрируем нового клиента
	clients[ws] = true

	for {
		var msg Message
		// Читаем новое сообщение от клиента
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Ошибка чтения: %v", err)
			delete(clients, ws)
			break
		}
		// Отправляем сообщение в broadcast канал
		broadcast <- msg
	}
}

func handleMessages() {
	for {
		// Достаем сообщение из канала
		msg := <-broadcast
		
		// Рассылаем всем подключенным клиентам
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("Ошибка записи: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}