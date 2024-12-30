package gcal

import (
	"context"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type Client struct {
	s *calendar.Service
}

func NewClient(ctx context.Context, credFile string) (*Client, error) {
	s, err := calendar.NewService(ctx, option.WithCredentialsFile(credFile))
	if err != nil {
		return nil, err
	}
	return &Client{s: s}, nil
}

func (c *Client) Events(ctx context.Context, calId string, from, to time.Time) ([]*calendar.Event, error) {
	var all []*calendar.Event
	pageToken := ""
	for {
		r, err := c.s.Events.List(calId).Context(ctx).PageToken(pageToken).TimeMin(from.Format(time.RFC3339)).TimeMax(to.Format(time.RFC3339)).ShowDeleted(false).SingleEvents(true).OrderBy("startTime").Do()
		if err != nil {
			return nil, err
		}
		all = append(all, r.Items...)
		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return all, nil
}
