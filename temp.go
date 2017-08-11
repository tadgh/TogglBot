package main

import "fmt"
import "log"
import "os"
import "errors"
import "strings"
import "unicode"
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

		commandArray := strings.Fields(strings.ToLower(incomingCommand.Event.Text))
		if strings.EqualFold(commandArray[0], "<@"+botId+">") {
			commandArray = commandArray[1:]
		}
		var reply ReplyChannel
		reply.Channel = incomingCommand.Channel

		switch commandArray[0] {
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
			replyChannel <- reply
		case "register":
			togglApiKey := commandArray[1]
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

		if reply.DisplayTitle != "" {
			continue
		}
		togglApiKey, ok := userMap[incomingCommand.Event.User]
		if !ok {
			reply.DisplayTitle = "You have not registered with togglbot yet. Try @togglbot register API_KEY_HERE"
			replyChannel <- reply
			continue
		}

		switch commandArray[0] {
		case "start":
			if len(commandArray) <= 2 {
				reply.DisplayTitle = "Please provide a project name and description! `@togglbot start PROJECT_NAME DESCRIPTION`"
				replyChannel <- reply
				break
			}
			project := commandArray[1]
			description := strings.Join(commandArray[2:], " ")
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
			if len(commandArray) < 3 {
				reply.DisplayTitle = "Sorry, I don't have enough information to make an event for you. try `@togglbot track PROJECT_NAME 9:00AM-5:00PM TASK_DESCRIPTION`"
				replyChannel <- reply
				break
			}
			projectName := commandArray[1]
			pid := getProjectWithName(togglApiKey, projectName)
			timeRange := commandArray[2]

			parsedDate := parseDate(commandArray[3])

			description := "no description provided"
			if len(commandArray) > 4 {
				description = strings.Join(commandArray[4:], " ")
			}

			startTime, duration, err := parseTimeRange(timeRange, parsedDate)
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

func parseDate(readingDate string) (*time.Date, error) {
	parsedDate, err := time.Parse("2006/01/02", readingDate)
	if err != nil {
		return nil, errors.New("Your date is incorrectly formatted! Try something like: 2017/08/11. The ISO 8601 Standard 😉")
	}

	return parsedDate
}

func parseTimeRange(timeRange string, parsedDate time.Date) (*time.Time, *time.Duration, error) {
	timeRange = strings.ToUpper(timeRange)
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

	if parsedDate == nil {
		dateToInput := time.Now()
	} else {
		dateToInput := parsedDate
	}

	// Kitchen time has only hours and PM/AM. Drop in today's date.
	fmt.Println("DATE:\t", dateToInput)
	startTime = time.Date(dateToInput.Year(), dateToInput.Month(), dateToInput.Day(), startTime.Hour(), startTime.Minute(), startTime.Second(), startTime.Nanosecond(), now.Location())
	fmt.Println("START TIME:\t", startTime)
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
			fmt.Println("FATAL SHIT")
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

				if isValidMessageEvent(event) {
					fmt.Println("Received event: ", event)
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

func isValidMessageEvent(event *slack.MessageEvent) bool {
	if event.Type != "message" {
		return false
	}
	if event.User == botId {
		return false
	}
	if strings.HasPrefix(event.Text, "<@"+botId+">") {
		return true
	}
	if strings.HasPrefix(event.Channel, "D") {
		return true
	}
	return false
}
