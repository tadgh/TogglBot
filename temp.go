package main

import "fmt"
import "net/http"
import "encoding/json"
import "io/ioutil"
import "golang.org/x/net/websocket"
import "sync/atomic"
import "log"
import "os"
import "strings"
import "gopkg.in/dougEfresh/gtoggl.v8"

var counter uint64

type responseSelf struct {
	Id string `json:"id"`
}

type responseRtmStart struct {
	Ok    bool         `json:"ok,omitEmpty"`
	Error string       `json:"error,omitEmpty"`
	Url   string       `json:"url,omitEmpty"`
	Self  responseSelf `json:"self,omitEmpty"`
	User  string       `json:"user,omitEmpty"`
}

func slackStart(token string) (wsurl, id string, err error) {
	url := fmt.Sprintf("https://slack.com/api/rtm.start?token=%s", token)
	resp, err := http.Get(url)
	if err != nil {
		return
	}

	if resp.StatusCode != 200 {
		err = fmt.Errorf("API request failed with code %d", resp.StatusCode)
		return
	}
	fmt.Println(json.MarshalIndent(resp.Body, "", "    "))
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return
	}

	var respObj responseRtmStart
	err = json.Unmarshal(body, &respObj)
	if err != nil {
		return
	}

	if !respObj.Ok {
		err = fmt.Errorf("Slack error: %s", respObj.Errorgopkg.in/dougEfresh/gtoggl.v8)
		return
	}

	wsurl = respObj.Url
	id = respObj.Self.Id
	return
}

type Message struct {
	Id      uint64 `json:"id"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

func getMessage(ws *websocket.Conn) (m Message, err error) {
	err = websocket.JSON.Receive(ws, &m)
	return
}

func postMessage(ws *websocket.Conn, m Message) error {
	m.Id = atomic.AddUint64(&counter, 1)
	return websocket.JSON.Send(ws, m)
}

func slackConnect(token string) (*websocket.Conn, string) {
	wsurl, id, err := slackStart(token)

	if err != nil {
		log.Fatal(err)
	}

	ws, err := websocket.Dial(wsurl, "", "https://api.slack.com")
	if err != nil {
		log.Fatal(err)
	}

	return ws, id

}

func pingTogglApi(apiKey string) error {
	tc, err := gtoggl.NewClient(apiKey)
	if err == nil {
		panic(err)
	}

}

func executeCommand(userMap map[string]string, userId string, parts []string) string {
	if parts[1] == "register" && len(parts) == 3 {
		userMap[userId] = parts[2]
		err = pingTogglApi(parts[2])
	}
}

func main() {
	userTogglMap := make(map[string]string)
	if len(os.Args) != 2 {
		fmt.Printf("usage: togglbot slack-bot-token")
		os.Exit(1)
	}

	ws, id := slackConnect(os.Args[1])
	fmt.Println("TogglBot ready, ^C exits")

	for {
		m, err := getMessage(ws)

		if err != nil {
			log.Fatal(err)
		}

		if m.Type == "message" && strings.HasPrefix(m.Text, "<@"+id+">") {
			//Try to parse it into a message
			parts := strings.Fields(m.Text)
			if len(parts) == 2 || len(parts) == 3 {
				go func(m Message) {
					m.Text = executeCommand(userMap)
					postMessage(ws, m)
				}(m)
			} else {
				m.Text = fmt.Sprintf("sorry, that does not compute!\n")
				fmt.Println(m.Text)
				//postMessage(ws, m)
			}

		}

	}

}
