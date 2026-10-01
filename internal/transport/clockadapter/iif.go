package clockadapter

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IIFTimeActivity is a normalized QuickBooks Desktop timer observation.
// Date is midnight in the explicitly supplied import location; no end time is
// inferred from Duration because an IIF time activity is not a punch pair.
type IIFTimeActivity struct {
	Employee     string
	Job          string
	Date         time.Time
	Duration     time.Duration
	Source       string
	SourceRecord string
}

// IIFBatch is a content-addressed collection of QuickBooks timer
// observations. ContentHash can be used as the batch idempotency key.
type IIFBatch struct {
	Activities  []IIFTimeActivity
	ContentHash string
	Unsupported []Unsupported
}

// ParseQuickBooksIIF parses QuickBooks Desktop timer IIF. The location is
// required because IIF dates have no offset. The variadic form keeps callers
// from accidentally passing a nil location while allowing a concise one-
// argument call in code that has already selected its location.
func ParseQuickBooksIIF(content []byte, locations ...*time.Location) (IIFBatch, error) {
	var zero IIFBatch
	if len(content) == 0 || len(content) > defaultMaxBody {
		return zero, fmt.Errorf("%w: IIF body", ErrMalformed)
	}
	if len(locations) != 1 || locations[0] == nil {
		return zero, fmt.Errorf("%w: IIF timezone is required", ErrMalformed)
	}
	digest := sha256.Sum256(content)
	out := IIFBatch{ContentHash: "sha256:" + hex.EncodeToString(digest[:])}
	r := csv.NewReader(strings.NewReader(string(content)))
	r.Comma = '\t'
	r.FieldsPerRecord = -1
	r.LazyQuotes = false
	var columns map[string]int
	rowNumber := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return zero, fmt.Errorf("%w: IIF row: %v", ErrMalformed, err)
		}
		rowNumber++
		if len(row) == 0 || strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		kind := normalize(row[0])
		switch kind {
		case "!timerhdr", "!timeact":
			if columns != nil {
				return zero, fmt.Errorf("%w: duplicate IIF header", ErrMalformed)
			}
			columns, err = iifColumns(row[1:])
			if err != nil {
				return zero, err
			}
		case "timerhdr", "timeact":
			if columns == nil {
				return zero, fmt.Errorf("%w: IIF header is required", ErrMalformed)
			}
			activity, unsupported, err := parseIIFActivity(row[1:], columns, locations[0], rowNumber)
			if err != nil {
				return zero, err
			}
			out.Activities = append(out.Activities, activity)
			out.Unsupported = append(out.Unsupported, unsupported...)
		default:
			return zero, fmt.Errorf("%w: unsupported IIF record %q", ErrMalformed, row[0])
		}
	}
	if columns == nil || len(out.Activities) == 0 {
		return zero, fmt.Errorf("%w: IIF has no time activities", ErrMalformed)
	}
	return out, nil
}

func iifColumns(raw []string) (map[string]int, error) {
	columns := make(map[string]int, len(raw))
	for i, field := range raw {
		key := normalize(field)
		if key == "" {
			return nil, fmt.Errorf("%w: empty IIF header", ErrMalformed)
		}
		if _, exists := columns[key]; exists {
			return nil, fmt.Errorf("%w: duplicate IIF field %q", ErrMalformed, field)
		}
		columns[key] = i
	}
	for _, required := range []string{"date", "emp", "job", "duration"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("%w: IIF field %s is required", ErrMalformed, required)
		}
	}
	return columns, nil
}

func parseIIFActivity(row []string, columns map[string]int, loc *time.Location, rowNumber int) (IIFTimeActivity, []Unsupported, error) {
	value := func(name string) (string, error) {
		i := columns[name]
		if i >= len(row) {
			return "", fmt.Errorf("%w: IIF row %d is short", ErrMalformed, rowNumber)
		}
		return strings.TrimSpace(row[i]), nil
	}
	dateText, err := value("date")
	if err != nil || dateText == "" {
		return IIFTimeActivity{}, nil, fmt.Errorf("%w: IIF date on row %d", ErrMalformed, rowNumber)
	}
	date, err := parseIIFDate(dateText, loc)
	if err != nil {
		return IIFTimeActivity{}, nil, fmt.Errorf("%w: IIF date on row %d", ErrMalformed, rowNumber)
	}
	employee, err := value("emp")
	if err != nil || employee == "" {
		return IIFTimeActivity{}, nil, fmt.Errorf("%w: IIF employee on row %d", ErrMalformed, rowNumber)
	}
	job, err := value("job")
	if err != nil || job == "" {
		return IIFTimeActivity{}, nil, fmt.Errorf("%w: IIF job on row %d", ErrMalformed, rowNumber)
	}
	durationText, err := value("duration")
	if err != nil {
		return IIFTimeActivity{}, nil, err
	}
	duration, err := parseIIFDuration(durationText)
	if err != nil {
		return IIFTimeActivity{}, nil, fmt.Errorf("%w: IIF duration on row %d", ErrMalformed, rowNumber)
	}
	unsupported := make([]Unsupported, 0)
	for name, i := range columns {
		if name == "date" || name == "emp" || name == "job" || name == "duration" {
			continue
		}
		if i < len(row) && strings.TrimSpace(row[i]) != "" {
			unsupported = append(unsupported, Unsupported{Field: name, Value: strings.TrimSpace(row[i])})
		}
	}
	sort.Slice(unsupported, func(i, j int) bool { return unsupported[i].Field < unsupported[j].Field })
	return IIFTimeActivity{Employee: employee, Job: job, Date: date, Duration: duration, Source: "quickbooks_iif", SourceRecord: fmt.Sprintf("timeact:%d", rowNumber)}, unsupported, nil
}

func parseIIFDate(raw string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "1/2/2006", "01/02/2006"} {
		if parsed, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date")
}

func parseIIFDuration(raw string) (time.Duration, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, fmt.Errorf("invalid duration")
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || hours < 0 || minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("invalid duration")
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute, nil
}
