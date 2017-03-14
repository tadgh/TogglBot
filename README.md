# TogglBot

TogglBot is a slack bot to help your organization interface with Toggl, the time-tracking application. 

build it with 
```go
go build temp.go
```
and then run it with 
```go
./temp SLACK_BOT_USER_API_TOKEN
```

Send DMs to Togglbot in order to interact with it.

Start a timer for a project with a description

Format:  `start PROJECT_NAME DESCRIPTION DESCRIPTION DESCRIPTION`
Example: `start myFancyProject working on TPS reports!`
