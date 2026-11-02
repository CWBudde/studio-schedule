package mcpserver

import (
	"context"
	"strconv"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func statsTools() []tool {
	return []tool{{
		name: "statistics",
		description: "The school's figures of a range of school dates, as the office's statistics page " +
			"(getStatistics; `stats`): the totals, by week (by month beyond 26 weeks), by class, room, course " +
			"type and instructor. range picks the current week and the 3 or 11 before it (last4Weeks, the " +
			"default; last12Weeks) or the current term up to today (term); from and to (both inclusive, at " +
			"most " + strconv.Itoa(service.MaxStatisticsDays) + " days) give a custom range. The top " +
			"participants are left out, as they name persons.",
		access:      httpapi.AccessOffice,
		annotations: readOnly("Statistics"),
		add:         handler(statistics),
	}}
}

// statisticsInput are the arguments of the statistics tool.
type statisticsInput struct {
	Range string `json:"range,omitempty" jsonschema:"last4Weeks (the default), last12Weeks, term, or custom with from and to."`
	From  string `json:"from,omitempty"  jsonschema:"The first school date of a custom range (YYYY-MM-DD), with to."`
	To    string `json:"to,omitempty"    jsonschema:"The last school date of a custom range, inclusive (YYYY-MM-DD), with from."`
}

// statisticsBody is the body of getStatistics without the top
// participants, who are persons. Its TopParticipants hides the embedded
// field of the same JSON name: encoding/json writes the less nested one,
// which omitempty leaves out, so the key is absent.
type statisticsBody struct {
	httpapi.Statistics

	TopParticipants []httpapi.ParticipantStatistics `json:"topParticipants,omitempty"`
}

// statistics is the statistics tool.
func statistics(ctx context.Context, s *Server, _ service.Principal, in statisticsInput) (any, error) {
	q, err := statisticsQuery(in)
	if err != nil {
		return nil, err
	}

	st, err := s.svc.Statistics.Statistics(ctx, q)
	if err != nil {
		return nil, err
	}

	return statisticsBody{Statistics: httpapi.NewStatistics(st)}, nil
}

// statisticsQuery is the query of in, as `stats` reads its flags: no range
// is last4Weeks, and from with to is a custom range. The service checks the
// range's name and a custom range's dates.
func statisticsQuery(in statisticsInput) (service.StatisticsQuery, error) {
	q := service.StatisticsQuery{Range: service.StatisticsRange(in.Range)}

	switch {
	case in.From == "" && in.To == "":
		if q.Range == "" {
			q.Range = service.StatisticsLast4Weeks
		}

		return q, nil
	case q.Range == "":
		q.Range = service.StatisticsCustom
	case q.Range != service.StatisticsCustom:
		return q, &domain.FieldError{
			Field: "range", Msg: "from and to give a custom range: leave range out or make it custom",
		}
	}

	var err error

	if q.From, err = parseDate("from", in.From); err != nil {
		return q, err
	}

	q.To, err = parseDate("to", in.To)

	return q, err
}
