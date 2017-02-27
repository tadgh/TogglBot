package main

import "fmt"
import "log"
import "os"
import "errors"
import "strings"
import "github.com/nlopes/slack"
import "github.com/gedex/go-toggl/toggl"

func pingTogglApi(apiKey string) error {
	tc := toggl.NewClient(apiKey)
	ws, err := tc.Workspaces.List()
	if err != nil {
		fmt.Println(os.Stderr, "Error: %s\n", err)
		return err
	}
	fmt.Println(ws)
	return nil
}

func stopTimer(apiKey string) (*toggl.TimeEntry, error) {
	tc := toggl.NewClient(apiKey)
	sessionId, ok := sessionMap[apiKey]
	if !ok {
		return nil, errors.New("no timer started!")
	}
	te, err := tc.TimeEntries.Stop(sessionId)
	if err != nil {
		return nil, err
	}
	return te, nil
}

func startTimer(apiKey string, pid int) *toggl.TimeEntry {
	tc := toggl.NewClient(apiKey)
	te := &toggl.TimeEntry{
		ProjectID:   pid,
		CreatedWith: "TogglBot",
	}
	tec, err := tc.TimeEntries.Start(te)
	if err != nil {
		fmt.Println(err)
	}
	sessionMap[apiKey] = tec.ID
	return tec
}

func getProjectWithName(apiKey, projectName string) int {
	tc := toggl.NewClient(apiKey)
	me, err := tc.Users.Me(true)
	var retVal int
	if err != nil {
		fmt.Println(err)
	}
	for _, project := range me.Projects {
		fmt.Println(project.Name)
		if strings.EqualFold(project.Name, projectName) {
			retVal = project.ID
		}
	}
	fmt.Println("FOUND PROJECT WITH NAME: " + projectName + ", id is: " + string(retVal))
	return retVal

}

type BotCommand struct {
	Channel string
	Event   *slack.MessageEvent
	UserId  string
}

type ReplyChannel struct {
	Channel      string
	Attachment   *slack.Attachment
	DisplayTitle string
}

var (
	api               *slack.Client
	botCommandChannel chan *BotCommand
	botReplyChannel   chan ReplyChannel
	botId             string
	userMap           map[string]string
	sessionMap        map[string]int
)

func handleBotCommands(replyChannel chan ReplyChannel) {
	commands := map[string]string{
		"register": "Register yourself with togglbot. `@togglbot register MY_TOGGL_API_KEY`",
		"start":    "Start a timer for a given project and description. `@togglbot start <PROJECT_NAME> <EVERYTHING_ELSE_IS_DESCRIPTION>`",
		"stop":     "Stops any current timer session. `@togglbot stop`",
		"track":    "adds a toggl entry to a project for a given time range. `@togglbot track icancope 9am-5pm`",
	}

	var reply ReplyChannel

	for {
		incomingCommand := <-botCommandChannel
		commandArray := strings.Fields(incomingCommand.Event.Text)
		reply.Channel = incomingCommand.Channel
		switch commandArray[1] {

		case "help":
			reply.DisplayTitle = "Help!"
			fields := make([]slack.AttachmentField, 0)
			for k, v := range commands {
				fields = append(fields, slack.AttachmentField{
					Title: "<bot> " + k,
					Value: v,
				})
			}
			attachment := &slack.Attachment{
				Pretext:    "TogglBot Command List",
				Color:      "#B733FF",
				Fields:     fields,
				MarkdownIn: []string{"fields"},
			}
			reply.Attachment = attachment
			fmt.Println("SENDING REPLY TO CHANNEL")
			replyChannel <- reply

		case "register":
			togglApiKey := commandArray[2]
			err := pingTogglApi(togglApiKey)
			if err != nil {
				reply.DisplayTitle = "Failed to register. Bad api key?"
			} else {
				userMap[incomingCommand.Event.User] = togglApiKey
				reply.DisplayTitle = "Successfully registered!"
			}
			replyChannel <- reply

		case "start":
			togglApiKey, ok := userMap[incomingCommand.Event.User]
			if !ok {
				reply.DisplayTitle = "You have not registered with togglbot yet. Try @togglbot register API_KEY_HERE"
				replyChannel <- reply
				break
			}
			project := commandArray[2]
			pid := getProjectWithName(togglApiKey, project)
			fmt.Printf("%v", pid)
			startTimer(togglApiKey, pid)
			reply.DisplayTitle = "Timer started! *get back to work peon*"
			replyChannel <- reply
		case "stop":
			togglApiKey, ok := userMap[incomingCommand.Event.User]
			if !ok {
				reply.DisplayTitle = "You have not registered with togglbot yet. Try @togglbot register API_KEY_HERE"
				replyChannel <- reply
				break
			}
			te, err := stopTimer(togglApiKey)
			if err != nil {
				reply.DisplayTitle = "couldn't stop timer: " + err.Error()
				replyChannel <- reply
				break
			}
			reply.DisplayTitle = fmt.Sprintf("Timer Stopped. Worked for %v minutes on %v", te.Duration, te.ProjectID)
			replyChannel <- reply
		}
	}
}

func handleBotReplies() {
	for {
		reply := <-botReplyChannel
		params := slack.PostMessageParameters{}
		params.AsUser = true
		if reply.Attachment != nil {
			params.Attachments = []slack.Attachment{*reply.Attachment}
		}
		_, _, err := api.PostMessage(reply.Channel, reply.DisplayTitle, params)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("usage: togglbot slack-bot-token")
		os.Exit(1)
	}
	userMap = make(map[string]string)
	sessionMap = make(map[string]int)
	token := os.Args[1]
	api = slack.New(token)
	api.SetDebug(true)
	rtm := api.NewRTM()

	botCommandChannel = make(chan *BotCommand)
	botReplyChannel = make(chan ReplyChannel)
	go rtm.ManageConnection()
	go handleBotCommands(botReplyChannel)
	go handleBotReplies()
	go fmt.Println("TogglBot ready, ^C exits")
Loop:
	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch event := msg.Data.(type) {
			case *slack.ConnectedEvent:
				botId = event.Info.User.ID
			case *slack.MessageEvent:
				var channelInfo *slack.Channel
				var channelName string
				if strings.HasPrefix(event.Channel, "D") {
					channelInfo = &slack.Channel{slack.groupConversation{
						Name: event.Channel,
					}}
				}

				botCommand := &BotCommand{
					Channel: channelInfo,
					Event:   event,
					UserId:  event.User,
				}

				if event.Type == "message" && strings.HasPrefix(event.Text, "<@"+botId+">") {
					fmt.Println("FOUND COMMAND")
					botCommandChannel <- botCommand
				}
			case *slack.RTMError:
				fmt.Printf("ERROR: %s\n", event.Error())
			case *slack.InvalidAuthEvent:
				fmt.Printf("Invalid credentials")
				break Loop
			default:
				fmt.Printf("\n")
			}
		}
	}

}
