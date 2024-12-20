package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jeinarsson/wall/gcal"
)

func main() {
	ctx := context.Background()

	calId := "9me0hsfh7oc365aqf39i9j6j38@group.calendar.google.com"
	c, err := gcal.NewClient(ctx, "../wall-gcloud.key")
	if err != nil {
		log.Fatal(err)
	}
	events, err := c.Events(ctx, calId, time.Now(), time.Now().AddDate(0, 0, 30))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Upcoming events:")
	if len(events) == 0 {
		fmt.Println("No upcoming events found.")
	} else {
		for _, item := range events {
			date := item.Start.DateTime
			if date == "" {
				date = item.Start.Date
			}
			fmt.Printf("%v (%v)\n", item.Summary, date)
		}
	}
}
