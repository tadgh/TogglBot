package main

import "fmt"
import "log"
import "os"
import "errors"
import "strings"
import "github.com/nlopes/slack"
import "github.com/tadgh/go-toggl"
import "encoding/gob"
import "time"
import "runtime"

const file = "./test.gob"

func Check(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Println(line, "\t", file, "\n", err)
		os.Exit(1)
	}
}

func pingTogglApi(apiKey string) error {
	ts := toggl.OpenSession(apiKey)
	_, err := ts.GetAccount()
	if err != nil {
		fmt.Println(os.Stderr, "Error: %s\n", err)
		return err
	}
	return nil
}

func Save(path string, object interface{}) error {
	file, err := os.Create(path)
	if err == nil {
		encoder := gob.NewEncoder(file)
		encoder.Encode(object)
	}
	file.Close()
	return err
}

func Load(path string, object interface{}) error {
	file, err := os.Open(path)
	if err == nil {
		decoder := gob.NewDecoder(file)
		err = decoder.Decode(object)
	}
	file.Close()
	return err
}

func createTimeEntry(apiKey, description string, start time.Time, duration time.Duration, pid, tid int) *toggl.TimeEntry {
	ts := toggl.OpenSession(apiKey)
	te, err := ts.CreateTimeEntry(pid, 0, start, duration, description)
	if err != nil {
		log.Fatal("Error uploading time entry! %v", err)
	}
	return &te
}

func stopTimer(apiKey string) (*toggl.TimeEntry, error) {
	ts := toggl.OpenSession(apiKey)
	te, err := ts.GetActiveTimeEntry()
	if err != nil {
		return nil, err
	}
	if te.ID <= 0 {
		return nil, errors.New("No timer is currently running!")
	}
	te, err = ts.StopTimeEntry(te)
	if err != nil {
		return nil, err
	}
	return &te, nil
}

func startTimer(apiKey, description string, pid int) *toggl.TimeEntry {
	ts := toggl.OpenSession(apiKey)
	te, err := ts.StartTimeEntryForProject(description, pid)

	if err != nil {
		fmt.Println(err)
	}
	sessionMap[apiKey] = te
	return &te
}

func getProjectWithName(apiKey, projectName string) int {
	ts := toggl.OpenSession(apiKey)
	acc, err := ts.GetAccount()
	var retVal int
	if err != nil {
		fmt.Println(err)
	}
	for _, project := range acc.Data.Projects {
		//case insensitive string comparison
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
	sessionMap        map[string]toggl.TimeEntry
)

func handleBotCommands(replyChannel chan ReplyChannel) {
	commands := map[string]string{
		"register": "Register yourself with togglbot. `@togglbot register MY_TOGGL_API_KEY`",
		"start":    "Start a timer for a given project and description. `@togglbot start <PROJECT_NAME> <EVERYTHING_ELSE_IS_DESCRIPTION>`",
		"stop":     "Stops any current timer session. `@togglbot stop`",
		"track":    "adds a toggl entry to a project for a given time range. `@togglbot track icancope 9am-5pm`",
	}

	for {
		incomingCommand := <-botCommandChannel
		commandArray := strings.Fields(incomingCommand.Event.Text)
		var reply ReplyChannel
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
				err := Save(file, userMap)
				Check(err)
				reply.DisplayTitle = "Successfully registered!"
			}
			replyChannel <- reply
		}

		togglApiKey, ok := userMap[incomingCommand.Event.User]
		if !ok {
			reply.DisplayTitle = "You have not registered with togglbot yet. Try @togglbot register API_KEY_HERE"
			replyChannel <- reply
			break
		}

		switch commandArray[1] {

		case "start":
			if len(commandArray) <= 3 {
				reply.DisplayTitle = "Please provide a project name and description! `@togglbot start PROJECT_NAME DESCRIPTION`"
				replyChannel <- reply
				break
			}
			project := commandArray[2]
			description := strings.Join(commandArray[3:], " ")
			pid := getProjectWithName(togglApiKey, project)
			fmt.Printf("%v", pid)
			startTimer(togglApiKey, description, pid)
			reply.DisplayTitle = "Timer started! *get back to work peon*"
			replyChannel <- reply
		case "stop":
			te, err := stopTimer(togglApiKey)
			if err != nil {
				reply.DisplayTitle = "couldn't stop timer: " + err.Error()
				replyChannel <- reply
				break
			}
			dur, err := time.ParseDuration(fmt.Sprintf("%vs", te.Duration))
			if err != nil {
				log.Fatal("Unparseable duration! %v", te.Duration)
			}
			reply.DisplayTitle = fmt.Sprintf("Timer Stopped. Worked for %v.", dur.String())
			replyChannel <- reply
		case "track":
			if len(commandArray) < 4 {
				reply.DisplayTitle = "Sorry, I don't have enough information to make an event for you. try `@togglbot track PROJECT_NAME 9:00AM-5:00PM TASK_DESCRIPTION`"
				replyChannel <- reply
				break
			}
			projectName := commandArray[2]
			pid := getProjectWithName(togglApiKey, projectName)
			timeRange := commandArray[3]
			description := "no description provided"
			if len(commandArray) > 4 {
				description = strings.Join(commandArray[4:], " ")
			}

			startTime, duration, err := parseTimeRange(timeRange)
			if err != nil {
				reply.DisplayTitle = err.Error()
				replyChannel <- reply
				break
			}
			createTimeEntry(togglApiKey, description, *startTime, *duration, pid, 0)
			reply.DisplayTitle = "Time entry created!"
			replyChannel <- reply
		default:
			reply.DisplayTitle = "Sorry, i don't understand that command. Try `@Togglbot help`"
			replyChannel <- reply
		}
	}
}

func parseTimeRange(timeRange string) (*time.Time, *time.Duration, error) {
	fields := strings.Split(timeRange, "-")
	if len(fields) != 2 {
		return nil, nil, errors.New("Your date range is incorrectly formatted! Try something like: 9:00AM-5:00PM")
	}

	startTime, err := time.Parse(time.Kitchen, fields[0])
	if err != nil {
		return nil, nil, errors.New("Your date range is incorrectly formatted! Try something like: 9:00AM-5:00PM")
	}

	endTime, err := time.Parse(time.Kitchen, fields[1])
	if err != nil {
		return nil, nil, errors.New("Your date range is incorrectly formatted! Try something like: 9:00AM-5:00PM")
	}

	duration := endTime.Sub(startTime)

	// Kitchen time has only hours and PM/AM. Drop in today's date.
	now := time.Now()
	startTime = startTime.AddDate(now.Year(), int(now.Month())-1, now.Day()-1)
	return &startTime, &duration, nil
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

	//First attempt to deserialize a saved registration file, otherwise
	//create new
	err := Load(file, &userMap)
	if err != nil {
		fmt.Println("ERROR WAS DETECTED")
		log.Fatal(err)
		userMap = make(map[string]string)
	}

	sessionMap = make(map[string]toggl.TimeEntry)
	token := os.Args[1]
	api = slack.New(token)
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

				botCommand := &BotCommand{
					Channel: event.Channel,
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
			}
		}
	}

}
