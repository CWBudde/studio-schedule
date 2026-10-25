package httpapi

import (
	"context"
	"encoding/hex"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ical"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// FeedPathPrefix starts the path of every calendar feed, outside BasePath.
const FeedPathPrefix = service.FeedPathPrefix

// FeedCacheControl lets calendars and browsers keep a feed for 15 minutes;
// only the token's holder may (private).
const FeedCacheControl = "private, max-age=900"

// registerCalendarFeeds registers the calendar feeds (ADR 0011): an
// account manages its subscription links under /my/calendar-feeds, and
// calendars fetch them, without a sign-in, at /ical/{token}.ics.
func registerCalendarFeeds(api huma.API, h *handlers) {
	register(api, AccessSignedIn, huma.Operation{
		OperationID: "listCalendarFeeds",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/calendar-feeds",
		Summary:     "List my calendar feeds",
		Description: "Returns the active calendar feeds of the signed-in account, oldest first. Their links " +
			"cannot be shown again: only a hash of the token is stored.",
		Tags: []string{"calendar feeds"},
	}, h.listCalendarFeeds)

	register(api, AccessSignedIn, withInvalid(api, huma.Operation{
		OperationID: "createCalendarFeed",
		Method:      http.MethodPost,
		Path:        BasePath + "/my/calendar-feeds",
		Summary:     "Create a calendar feed",
		Description: "Creates the subscription link of a subject: `bookings` (the lessons the account and its " +
			"dependents are booked for), `teaching` (the lessons of the account's staff records; staff only, " +
			"else 403) or `schedule` (the schedule as the account sees it, of one location with `locationId`; " +
			"an unknown location is a 422). An account has one link per subject: a new one revokes the old. " +
			"The token and the link are in this response only. Not while an admin is signed in as the " +
			"account (403).",
		Tags:          []string{"calendar feeds"},
		DefaultStatus: http.StatusCreated,
	}), h.createCalendarFeed)

	register(api, AccessSignedIn, huma.Operation{
		OperationID:   "revokeCalendarFeed",
		Method:        http.MethodDelete,
		Path:          BasePath + "/my/calendar-feeds/{feedId}",
		Summary:       "Revoke a calendar feed",
		Description:   "Revokes an active feed of the signed-in account: its link answers 404 from now on.",
		Tags:          []string{"calendar feeds"},
		DefaultStatus: http.StatusNoContent,
	}, h.revokeCalendarFeed)

	register(api, AccessPublic, huma.Operation{
		OperationID: "getCalendarFeed",
		Method:      http.MethodGet,
		Path:        FeedPathPrefix + "{token}" + service.FeedPathSuffix,
		Summary:     "Fetch a calendar feed",
		Description: "Returns the iCalendar document (RFC 5545) of a feed, as its account may see it now: the " +
			"lessons from four weeks back on, in the school's time zone, canceled ones with " +
			"`STATUS:CANCELLED`. The token is the proof; no sign-in is needed. An unknown or revoked token, " + //nolint:misspell // RFC 5545
			"or the feed of a disabled account, is a 404. With the `ETag` in `If-None-Match` an unchanged " +
			"feed is a 304. `download=true` asks for an attachment with a file name. Rate-limited per " +
			"client, generously since calendar services poll from shared addresses, and per feed.",
		Tags: []string{"calendar feeds"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The feed.",
				Content:     map[string]*huma.MediaType{"text/calendar": {Schema: &huma.Schema{Type: "string"}}},
			},
			"304":     {Description: "The feed did not change since the ETag in If-None-Match."},
			"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
		},
	}, h.getCalendarFeed)
}

// CalendarFeed is a calendar feed of the signed-in account, without its
// token.
type CalendarFeed struct {
	ID           domain.CalendarFeedID `json:"id"`
	Scope        domain.FeedScope      `doc:"What the feed shows." enum:"bookings,teaching,schedule" json:"scope"`
	LocationID   domain.LocationID     `doc:"The location of a schedule feed of one location." json:"locationId,omitzero"`
	LocationName string                `doc:"The name of that location." json:"locationName,omitempty"`
	CreatedAt    time.Time             `doc:"When the link was created (UTC)." json:"createdAt"`
	LastUsedAt   time.Time             `doc:"When a calendar last fetched the feed (UTC), updated at most once a day; absent if never." json:"lastUsedAt,omitzero"`
}

func calendarFeedBody(info service.CalendarFeedInfo) CalendarFeed {
	f := info.Feed

	return CalendarFeed{
		ID: f.ID, Scope: f.Scope, LocationID: f.LocationID, LocationName: info.LocationName,
		CreatedAt: f.CreatedAt.UTC(), LastUsedAt: f.LastUsedAt.UTC(),
	}
}

// CalendarFeedList is the response body of listCalendarFeeds.
type CalendarFeedList struct {
	Items []CalendarFeed `doc:"Active feeds, oldest first." json:"items" nullable:"false"`
}

// CalendarFeedListOutput is the response of listCalendarFeeds.
type CalendarFeedListOutput struct {
	Body CalendarFeedList
}

func (h *handlers) listCalendarFeeds(ctx context.Context, _ *struct{}) (*CalendarFeedListOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	feeds, err := h.deps.Feeds.List(ctx, p)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &CalendarFeedListOutput{Body: CalendarFeedList{Items: make([]CalendarFeed, 0, len(feeds))}}
	for _, f := range feeds {
		out.Body.Items = append(out.Body.Items, calendarFeedBody(f))
	}

	return out, nil
}

// CreateCalendarFeedInput is the request of createCalendarFeed.
type CreateCalendarFeedInput struct {
	Body struct {
		Scope      domain.FeedScope  `doc:"What the feed shows." enum:"bookings,teaching,schedule" json:"scope"`
		LocationID domain.LocationID `doc:"Only for scope schedule: the location whose lessons the feed shows; absent: every location." json:"locationId,omitzero" required:"false"`
	}
}

// NewCalendarFeed is the response body of createCalendarFeed.
type NewCalendarFeed struct {
	Feed CalendarFeed `json:"feed"`
	// The secret is shown only here.
	Token string `doc:"The feed's secret token; it is not shown again."                                          json:"token"`
	Path  string `doc:"The feed's path below the app's origin, /ical/{token}.ics."                             json:"path"`
	URL   string `doc:"The feed's absolute URL to subscribe to; absent if the server does not know its own URL, and in a demo installation (calendars cannot reach the web demo): download the path instead." json:"url,omitempty"`
}

// NewCalendarFeedOutput is the response of createCalendarFeed.
type NewCalendarFeedOutput struct {
	Body NewCalendarFeed
}

func (h *handlers) createCalendarFeed(ctx context.Context, in *CreateCalendarFeedInput) (*NewCalendarFeedOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	feed, err := h.deps.Feeds.Create(ctx, p, in.Body.Scope, in.Body.LocationID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &NewCalendarFeedOutput{Body: NewCalendarFeed{
		Feed: calendarFeedBody(feed.CalendarFeedInfo), Token: feed.Token, Path: feed.Path, URL: feed.URL,
	}}, nil
}

// CalendarFeedIDInput names a calendar feed.
type CalendarFeedIDInput struct {
	FeedID domain.CalendarFeedID `doc:"The feed." path:"feedId"`
}

func (h *handlers) revokeCalendarFeed(ctx context.Context, in *CalendarFeedIDInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	if err := h.deps.Feeds.Revoke(ctx, p, in.FeedID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

// CalendarFeedInput is the request of getCalendarFeed.
type CalendarFeedInput struct {
	Token       string `doc:"The feed's token."                                         path:"token"`
	Download    bool   `doc:"Send the feed as an attachment with a file name."          query:"download"`
	IfNoneMatch string `doc:"The ETag of the feed the client has; unchanged: a 304."    header:"If-None-Match"`
}

// CalendarFeedOutput is the response of getCalendarFeed: the document, or
// nothing with status 304.
type CalendarFeedOutput struct {
	Status             int
	ContentType        string `header:"Content-Type"`
	ETag               string `header:"ETag"`
	CacheControl       string `header:"Cache-Control"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

func (h *handlers) getCalendarFeed(ctx context.Context, in *CalendarFeedInput) (*CalendarFeedOutput, error) {
	if err := allow(h.deps.RateLimits.CalendarFeeds, clientIP(ctx)); err != nil {
		return nil, err
	}

	// By the token's hash: the limiter keeps no secrets.
	tokenHash := domain.HashToken(in.Token)
	if err := allow(h.deps.RateLimits.CalendarFeedTokens, hex.EncodeToString(tokenHash[:])); err != nil {
		return nil, err
	}

	doc, err := h.deps.Feeds.Render(ctx, in.Token)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &CalendarFeedOutput{ETag: `"` + doc.ETag + `"`, CacheControl: FeedCacheControl}

	if etagMatches(in.IfNoneMatch, out.ETag) {
		out.Status = http.StatusNotModified

		return out, nil
	}

	out.Status, out.ContentType, out.Body = http.StatusOK, ical.ContentType, doc.Body

	out.ContentDisposition = "inline"
	if in.Download {
		out.ContentDisposition = `attachment; filename="` + doc.Filename + `"`
	}

	return out, nil
}

// etagMatches reports whether the If-None-Match header lists etag (or is
// "*"), comparing weakly as RFC 9110 asks for If-None-Match.
func etagMatches(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}

	return false
}
