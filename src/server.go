package main

import (
	"flag"
	"text/template"
	"path/filepath"
	"net/http"
	"log"
	"go/build"
	"github.com/gorilla/websocket"
)

type hub struct {
	connections map[*connection]bool
	broadcast chan []byte
	register chan *connection
	unregister chan *connection
}

func newHub() *hub {
	return &hub {
		connections: make(map[*connection]bool),
		broadcast: make(chan []byte),
		register: make(chan *connection),
		unregister: make(chan *connection),
	}
}

func (h *hub) run() {
	for {
		select {
		case c := <-h.register:
			h.connections[c] = true
		case c := <-h.unregister:
			if _, ok := h.connections[c]; ok {
				delete(h.connections, c)
				close(c.send)
			}
		case m := <-h.broadcast:
			for c := range h.connections {
				select {
				case c.send <- m:
				default:
					delete(h.connections, c)
					close(c.send)
				}
			}
		}
	}
}

type connection struct {
	ws *websocket.Conn
	send chan []byte
	h *hub
}

func (c *connection) reader() {
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			break
		}
		c.h.broadcast <- message
	}
	c.ws.Close()
}

func (c *connection) writer() {
	for message := range c.send {
		err := c.ws.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			break
		}
	}
	c.ws.Close()
}

var upgrader = &websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

type wsHandler struct {
	h *hub
}

func (wsh wsHandler) ServeHTTP (w http.ResponseWriter, r *http.Request) {
	ws , err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &connection {send: make(chan []byte, 256), ws: ws, h: wsh.h}
	c.h.register <- c
	defer func() { c.h.unregister <- c}()
	go c.writer()
	c.reader()
}

var (
	addr = flag.String("addr", ":8080", "http service address")
	assets = flag.String("assets", defaultAssetPath(), "path to assets")
	homeTempl *template.Template
)

func defaultAssetPath() string {
	p, err := build.Default.Import("./", "", build.FindOnly)
	if err != nil {
		return "./"
	}
	return p.Dir
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	homeTempl.Execute(w, r.Host)
}

func main() {
	flag.Parse()
	homeTempl = template.Must(template.ParseFiles(filepath.Join(*assets, "home.html")))
	h := newHub()
	go h.run()
	http.HandleFunc("/", homeHandler)
	http.Handle("/ws", wsHandler{h: h})
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
