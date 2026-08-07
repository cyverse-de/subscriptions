package timestamp

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

const (
	DateOnly      = time.DateOnly
	DateTimeLocal = "2006-01-02T15:04:05"
	RFC3339       = time.RFC3339
)

var (
	DateOnlyRegexp      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	DateTimeLocalRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$`)
	RFC3339Regexp       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$`)
)

type Timestamp time.Time

// layoutForValue returns the layout to use for a given timestamp value.
func layoutForValue(value string) (string, error) {
	switch {
	case DateOnlyRegexp.MatchString(value):
		return DateOnly, nil
	case DateTimeLocalRegexp.MatchString(value):
		return DateTimeLocal, nil
	case RFC3339Regexp.MatchString(value):
		return RFC3339, nil
	default:
		return "", fmt.Errorf("unrecognized timestamp layout: %s", value)
	}
}

// Parse attempts to parse the given value as a timestamp. The timestamp will be parsed in the time zone of the
// current location unless the time zone is included in the timestamp itself. The accepted formats are:
//
//	2024-02-21                - Midnight on the specified date in the local time zone.
//	2024-02-21T01:02:03       - The specified date and time in the local time zone.
//	2024-02-21T01:02:03Z      - The specified date and time in UTC.
//	2024-02-01T01:02:03-07:00 - The specified date and time in the specified time zone.
func Parse(value string) (Timestamp, error) {
	var t time.Time

	// Determine the timestamp layout.
	layout, err := layoutForValue(value)
	if err != nil {
		return Timestamp(t), err
	}

	// Parse the timestamp.
	t, err = time.ParseInLocation(layout, value, time.Now().Location())
	return Timestamp(t), err
}

// UnmarshalJSON converts a JSON to a timestamp.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	value := string(data)

	// Ignore empty values.
	if value == "null" || value == `""` {
		return nil
	}

	// Unquote the string.
	value, err := strconv.Unquote(value)
	if err != nil {
		return err
	}

	// Parse the timestamp.
	*t, err = Parse(value)
	return err
}
