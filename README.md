# ![](https://pbs.twimg.com/profile_images/3224183389/017e79a95ca24d46f1794e9b2d6209ed_normal.png) TogglBot ![](https://pbs.twimg.com/profile_images/378800000271328329/349dc6f270e53cbe09cd05f6c032fc67_normal.png)

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

## Register yourself with Togglbot

Format: `register TOGGL_API_TOKEN`

Example: `register 466b5497ec191008f3d7ad482602fbbb`

## Start a timer for a project with a description 

Format:  `start PROJECT_NAME DESCRIPTION DESCRIPTION DESCRIPTION`

Example: `start myFancyProject working on TPS reports!`

## Stop the active timer

Format:  `stop`

Example: `stop`

## Create a time entry with defined start/end time. 

Format:  `track PROJECT_NAME START_TIME-END_TIME DESCRIPTION DESCRIPTION`

Example: `track myFancyProject 9:00AM-5:00PM working on TPS reports!`

_Note: all times are tracked in local time of wherever you are running togglbot._

_Note: Times must be in [Kitchen Time](https://github.com/golang/go/blob/7ad512e7ffe576c4894ea84b02e954846fbda643/src/time/format.go#L75) (3:04PM)_
